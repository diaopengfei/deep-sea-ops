// Package scanner 实现后台自动扫描调度器。
//
// v0.5.2: 每 10 分钟对所有在线 Agent 下发 SCAN_PROJECTS 指令,
// 扫描完成后自动触发配置比对(对 Spring 项目),
// 结果持久化到 Raft 供前端查询。
//
// v0.5.3: 增加 per-agent 互斥锁, 防止后台扫描与手动扫描并发竞态;
// 配置比对结果持久化到 ProjectRecord.ConfigDiffJSON;
// 从 effectiveConfig 提取 Nacos 认证参数(username/password/accessToken)。
package scanner

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/deepsea-ops/server/internal/agentclient"
	"github.com/deepsea-ops/server/internal/configdiff"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// Scheduler 是后台自动扫描调度器。
type Scheduler struct {
	store    *store.Store
	grpcSrv  *grpcserver.Server
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once

	// per-agent 互斥锁, 防止后台扫描与手动扫描并发执行导致数据覆盖
	agentMu   sync.Map // map[string]*sync.Mutex
}

// NewScheduler 创建扫描调度器。interval 为扫描周期, 建议 10 分钟。
func NewScheduler(s *store.Store, gs *grpcserver.Server, interval time.Duration) *Scheduler {
	return &Scheduler{
		store:    s,
		grpcSrv:  gs,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start 启动后台扫描 goroutine。
func (sc *Scheduler) Start() {
	go sc.run()
	log.Printf("[扫描调度器] 已启动, 间隔 %v", sc.interval)
}

// Stop 停止扫描调度器。可安全多次调用。
func (sc *Scheduler) Stop() {
	sc.stopOnce.Do(func() {
		close(sc.stopCh)
	})
}

// getAgentMu 获取(或创建)指定 Agent 的互斥锁。
// 同一 Agent 的扫描串行执行, 不同 Agent 并行, 兼顾安全与效率。
func (sc *Scheduler) getAgentMu(agentID string) *sync.Mutex {
	v, _ := sc.agentMu.LoadOrStore(agentID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// run 是调度器主循环。
func (sc *Scheduler) run() {
	// 启动后先等 30 秒, 让 Agent 有时间连上
	select {
	case <-time.After(30 * time.Second):
	case <-sc.stopCh:
		return
	}

	// 首次立即执行一次
	sc.scanAll()

	ticker := time.NewTicker(sc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sc.scanAll()
		case <-sc.stopCh:
			log.Println("[扫描调度器] 已停止")
			return
		}
	}
}

// scanAll 对所有在线 Agent 执行扫描 + 配置比对。
func (sc *Scheduler) scanAll() {
	agents := sc.grpcSrv.ListAgents()
	if len(agents) == 0 {
		return
	}
	log.Printf("[扫描调度器] 开始扫描 %d 个在线 Agent", len(agents))

	for _, a := range agents {
		sc.scanAgent(a.ID)
	}
	log.Printf("[扫描调度器] 扫描完成")
}

// scanAgent 对单个 Agent 执行扫描 + 配置比对。
// v0.5.3: 用 per-agent 互斥锁防止与手动扫描并发。
func (sc *Scheduler) scanAgent(agentID string) {
	mu := sc.getAgentMu(agentID)
	if !mu.TryLock() {
		log.Printf("[扫描调度器] Agent %s 正在扫描中(手动或其他任务持有锁), 跳过本次", agentID)
		return
	}
	defer mu.Unlock()

	// 1. 下发扫描指令
	scanResultJSON, err := sc.grpcSrv.ScanProjects(agentID, "", 60*time.Second)
	if err != nil {
		log.Printf("[扫描调度器] Agent %s 扫描失败: %v", agentID, err)
		return
	}

	// 2. 解析扫描结果
	var result struct {
		Projects []agentclient.ProjectInfo `json:"projects"`
		Hosts    string                    `json:"hosts"`
		HostsErr string                    `json:"hostsErr"`
	}
	if err := json.Unmarshal([]byte(scanResultJSON), &result); err != nil {
		log.Printf("[扫描调度器] Agent %s 解析扫描结果失败: %v", agentID, err)
		return
	}

	// 3. 持久化项目记录到 Raft
	if err := sc.store.ClearAgentProjects(agentID); err != nil {
		log.Printf("[扫描调度器] Agent %s 清除旧项目记录失败: %v", agentID, err)
	}
	now := time.Now()
	for _, p := range result.Projects {
		rec := model.ProjectRecord{
			ID:          agentID + "|" + p.Path,
			AgentID:     agentID,
			Path:        p.Path,
			Type:        string(p.Type),
			Name:        p.Name,
			ConfigFiles: p.ConfigFiles,
			JarPath:     p.JarPath,
			JarEntry:    p.JarEntry,
			Running:     p.Running,
			PID:         p.PID,
			ScannedAt:   now,
		}
		if err := sc.store.AddProject(rec); err != nil {
			log.Printf("[扫描调度器] Agent %s 持久化项目 %s 失败: %v", agentID, rec.ID, err)
		}
	}

	// 4. 对 Spring 项目自动触发配置比对
	for _, p := range result.Projects {
		if p.Type != agentclient.ProjectJavaSpring {
			continue
		}
		sc.autoConfigDiff(agentID, p)
	}
}

// autoConfigDiff 对单个项目自动执行配置比对。
// v0.5.3: 从 effectiveConfig 提取 Nacos 认证参数(username/password/accessToken),
// 比对结果持久化到 ProjectRecord.ConfigDiffJSON 供前端查询。
func (sc *Scheduler) autoConfigDiff(agentID string, p agentclient.ProjectInfo) {
	// 从 effectiveConfig 提取 Nacos 地址
	nacosAddr := extractNacosAddr(p.EffectiveConfig)
	if nacosAddr == "" {
		log.Printf("[扫描调度器] Agent %s 项目 %s 未找到 Nacos 地址, 跳过配置比对", agentID, p.Name)
		return
	}

	// 确定本地配置文件路径(取第一个配置文件)
	localPath := ""
	if len(p.ConfigFiles) > 0 {
		localPath = p.ConfigFiles[0]
	}

	// v0.5.3: 提取 Nacos 认证参数(从 effectiveConfig)
	params := map[string]string{
		"nacosAddr":        nacosAddr,
		"nacosDataId":      extractNacosConfig(p.EffectiveConfig, "spring.application.name") + ".yml",
		"nacosGroup":       extractNacosConfig(p.EffectiveConfig, "spring.cloud.nacos.config.group"),
		"nacosNamespace":   extractNacosConfig(p.EffectiveConfig, "spring.cloud.nacos.config.namespace"),
		"nacosUsername":    extractNacosConfig(p.EffectiveConfig, "spring.cloud.nacos.username"),
		"nacosPassword":    extractNacosConfig(p.EffectiveConfig, "spring.cloud.nacos.password"),
		"nacosAccessToken": extractNacosConfig(p.EffectiveConfig, "spring.cloud.nacos.config.access-token"),
		"localPath":        localPath,
		"jarPath":          p.JarPath,
		"jarEntry":         p.JarEntry,
	}

	snapJSON, err := sc.grpcSrv.CollectConfigs(agentID, params, 30*time.Second)
	if err != nil {
		log.Printf("[扫描调度器] Agent %s 项目 %s 配置采集失败: %v", agentID, p.Name, err)
		return
	}

	report := configdiff.BuildReport(snapJSON)
	diffCount := len(report.OnlyNacos) + len(report.OnlyLocal) + len(report.OnlyJar) +
		len(report.NacosLocal) + len(report.NacosJar) + len(report.LocalJar)
	log.Printf("[扫描调度器] Agent %s 项目 %s 配置比对完成: 一致 %d, 差异 %d",
		agentID, p.Name, len(report.Consistent), diffCount)

	// v0.5.3: 持久化比对结果到 Raft, 供前端查询
	reportJSON, err := json.Marshal(report)
	if err != nil {
		log.Printf("[扫描调度器] Agent %s 项目 %s 序列化比对结果失败: %v", agentID, p.Name, err)
		return
	}
	projectID := agentID + "|" + p.Path
	upd := &store.ConfigDiffUpdate{
		ProjectID:     projectID,
		ConfigDiff:    string(reportJSON),
		DiffScannedAt: time.Now().UnixMilli(),
	}
	if err := sc.store.SetConfigDiff(upd); err != nil {
		log.Printf("[扫描调度器] Agent %s 项目 %s 持久化比对结果失败: %v", agentID, p.Name, err)
	}
}

// extractNacosAddr 从 effectiveConfig 中提取 Nacos 地址。
func extractNacosAddr(ec *agentclient.EffectiveConfig) string {
	if ec == nil {
		return ""
	}
	// 尝试常见的 Nacos 配置 key
	keys := []string{
		"spring.cloud.nacos.config.server-addr",
		"spring.cloud.nacos.discovery.server-addr",
		"spring.cloud.nacos.server-addr",
	}
	for _, k := range keys {
		if v := findConfigValue(ec, k); v != "" {
			return v
		}
	}
	return ""
}

// extractNacosConfig 从 effectiveConfig 中提取指定 key 的值。
func extractNacosConfig(ec *agentclient.EffectiveConfig, key string) string {
	return findConfigValue(ec, key)
}

// findConfigValue 在生效配置项中按 key 查找值。
func findConfigValue(ec *agentclient.EffectiveConfig, key string) string {
	if ec == nil {
		return ""
	}
	for _, item := range ec.Items {
		if item.Key == key {
			return item.Value
		}
	}
	return ""
}
