// Package monitor 实现资源监控的定时采集与阈值告警。
//
// 组成:
//   - Collector: 每 30s 向所有在线 Agent 下发 COLLECT_METRICS, 结果存 metrics.Store
//   - AlertEngine: 基于 metrics.Store 评估告警规则, 触发时走 Webhook
//
// 设计原则:
//   - 仅 Leader 节点运行(Agent 只连 Leader), Follower 上为空操作
//   - 采集失败不告警(避免 Agent 重连瞬间误报), 只记日志
//   - 告警去抖: 持续超阈值达设定时长才触发, 恢复后发 resolved
package monitor

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/deepsea-ops/server/internal/agentclient"
	"github.com/deepsea-ops/server/internal/grpcserver"
	"github.com/deepsea-ops/server/internal/metrics"
)

// Collector 定时向在线 Agent 下发 COLLECT_METRICS, 把完整指标存入 metrics.Store。
type Collector struct {
	grpcSrv  *grpcserver.Server
	store    *metrics.Store
	interval time.Duration
	timeout  time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewCollector 创建采集器。interval 为采集周期(建议 30s), timeout 为单 Agent 超时。
func NewCollector(gs *grpcserver.Server, ms *metrics.Store, interval, timeout time.Duration) *Collector {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Collector{
		grpcSrv:  gs,
		store:    ms,
		interval: interval,
		timeout:  timeout,
		stopCh:   make(chan struct{}),
	}
}

// Start 启动后台采集 goroutine。
func (c *Collector) Start() {
	go c.run()
	log.Printf("[监控采集器] 已启动, 间隔 %v", c.interval)
}

// Stop 停止采集器。
func (c *Collector) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
}

func (c *Collector) run() {
	// 启动后等 20s, 让 Agent 稳定连上(与扫描调度器错开)
	select {
	case <-time.After(20 * time.Second):
	case <-c.stopCh:
		return
	}
	c.collectAll()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.collectAll()
		case <-c.stopCh:
			log.Println("[监控采集器] 已停止")
			return
		}
	}
}

// collectAll 遍历在线 Agent, 并发下发采集指令, 结果存 Store。
func (c *Collector) collectAll() {
	agents := c.grpcSrv.ListAgents()
	if len(agents) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, a := range agents {
		wg.Add(1)
		go func(agentID string) {
			defer wg.Done()
			c.collectOne(agentID)
		}(a.ID)
	}
	wg.Wait()
}

// collectOne 对单个 Agent 采集并存 Store。
func (c *Collector) collectOne(agentID string) {
	out, err := c.grpcSrv.SendCommand(agentID, "COLLECT_METRICS", nil, c.timeout)
	if err != nil {
		// 采集失败只记日志, 不触发告警(避免重连瞬间误报)
		log.Printf("[监控采集器] Agent %s 采集失败: %v", agentID, err)
		return
	}
	var m agentclient.Metrics
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		log.Printf("[监控采集器] Agent %s 解析指标失败: %v", agentID, err)
		return
	}
	c.store.Record(agentID, m)
}
