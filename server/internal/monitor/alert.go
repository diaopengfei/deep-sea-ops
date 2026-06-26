package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/deepsea-ops/server/internal/agentclient"
	"github.com/deepsea-ops/server/internal/metrics"
)

// Rule 是单条告警规则。
// Metric 字段支持: "cpu" / "memory" / "disk", 对应各 Percent 字段。
// 持续时长内(基于采样点)所有点超阈值才触发, 避免毛刺误报。
type Rule struct {
	Name        string  `yaml:"name" json:"name"`         // 规则名
	Metric      string  `yaml:"metric" json:"metric"`     // cpu / memory / disk
	Threshold   float64 `yaml:"threshold" json:"threshold"` // 阈值百分比如 80
	DurationSec int     `yaml:"durationSec" json:"durationSec"` // 持续秒数如 300(5分钟)
}

// AlertEvent 是一次告警/恢复事件。
type AlertEvent struct {
	AgentID   string    `json:"agentId"`
	Rule      Rule      `json:"rule"`
	Value     float64   `json:"value"`     // 触发时的指标值
	Status    string    `json:"status"`    // firing / resolved
	FiredAt   time.Time `json:"firedAt"`
	ResolvedAt time.Time `json:"resolvedAt,omitempty"`
}

// WebhookConfig 是告警通知的 Webhook 配置。
// Type 支持: dingtalk / feishu / wechat / generic。
// generic 直接 POST JSON 到 URL; 前三者按各自机器人格式发送。
type WebhookConfig struct {
	Type string `yaml:"type" json:"type"` // dingtalk/feishu/wechat/generic
	URL  string `yaml:"url" json:"url"`
}

// Notifier 发送告警通知。接口化便于扩展(邮件、Slack 等)。
type Notifier interface {
	Notify(event AlertEvent) error
}

// AlertEngine 告警评估引擎。周期性扫描 metrics.Store, 评估规则, 触发告警。
type AlertEngine struct {
	store    *metrics.Store
	rules    []Rule
	notifier Notifier
	interval time.Duration

	mu    sync.Mutex
	// agentID -> ruleName -> 状态(首次超阈值时间 / 是否已 firing)
	states map[string]map[string]*alertState

	stopCh   chan struct{}
	stopOnce sync.Once
}

type alertState struct {
	firstOverAt time.Time // 首次超阈值时间
	firing      bool      // 是否已发送 firing
	firedValue  float64   // 触发时的值
}

// NewAlertEngine 创建告警引擎。interval 为评估周期(建议 30s, 与采集对齐)。
// notifier 为 nil 时不发送通知(仅记日志), 便于未配置 Webhook 时运行。
func NewAlertEngine(ms *metrics.Store, rules []Rule, notifier Notifier, interval time.Duration) *AlertEngine {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &AlertEngine{
		store:    ms,
		rules:    rules,
		notifier: notifier,
		interval: interval,
		states:   make(map[string]map[string]*alertState),
		stopCh:   make(chan struct{}),
	}
}

// Start 启动告警评估循环。
func (e *AlertEngine) Start() {
	if len(e.rules) == 0 {
		log.Println("[告警引擎] 未配置告警规则, 不启动评估")
		return
	}
	go e.run()
	log.Printf("[告警引擎] 已启动, 规则 %d 条, 评估间隔 %v", len(e.rules), e.interval)
}

// Stop 停止告警引擎。
func (e *AlertEngine) Stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
}

func (e *AlertEngine) run() {
	// 等采集器先跑一轮
	select {
	case <-time.After(35 * time.Second):
	case <-e.stopCh:
		return
	}
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.evaluate()
		case <-e.stopCh:
			log.Println("[告警引擎] 已停止")
			return
		}
	}
}

// evaluate 评估所有 Agent 的所有规则。
func (e *AlertEngine) evaluate() {
	for _, agentID := range e.store.Agents() {
		history := e.store.History(agentID)
		for _, rule := range e.rules {
			e.evalAgentRule(agentID, rule, history)
		}
	}
}

// evalAgentRule 评估单 Agent 单规则。
func (e *AlertEngine) evalAgentRule(agentID string, rule Rule, history []metrics.Sample) {
	if len(history) == 0 {
		return
	}
	// 取持续时长内的采样点。采样间隔约 30s, durationSec/30 = 点数。
	// 为容错, 取末尾 durationSec 内的所有点。
	cutoff := time.Now().Add(-time.Duration(rule.DurationSec) * time.Second)
	var recent []metrics.Sample
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Time.Before(cutoff) {
			break
		}
		recent = append([]metrics.Sample{history[i]}, recent...)
	}
	if len(recent) == 0 {
		return
	}

	// 全部点超阈值才算"持续超阈值"
	allOver := true
	lastVal := 0.0
	for _, s := range recent {
		v := metricValue(s.Metrics, rule.Metric)
		lastVal = v
		if v < rule.Threshold {
			allOver = false
			break
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	agentStates, ok := e.states[agentID]
	if !ok {
		agentStates = make(map[string]*alertState)
		e.states[agentID] = agentStates
	}
	st, ok := agentStates[rule.Name]
	if !ok {
		st = &alertState{}
		agentStates[rule.Name] = st
	}

	if allOver {
		if st.firstOverAt.IsZero() {
			st.firstOverAt = time.Now()
		}
		// 持续时长满足且未发过 firing → 触发
		if !st.firing && time.Since(st.firstOverAt) >= time.Duration(rule.DurationSec)*time.Second {
			st.firing = true
			st.firedValue = lastVal
			ev := AlertEvent{
				AgentID: agentID, Rule: rule, Value: lastVal,
				Status: "firing", FiredAt: time.Now(),
			}
			e.fireNotify(ev)
		}
	} else {
		// 恢复: 之前 firing 则发 resolved
		if st.firing {
			ev := AlertEvent{
				AgentID: agentID, Rule: rule, Value: lastVal,
				Status: "resolved", FiredAt: st.firstOverAt, ResolvedAt: time.Now(),
			}
			e.fireNotify(ev)
		}
		st.firstOverAt = time.Time{}
		st.firing = false
	}
}

// fireNotify 发送通知, 失败只记日志不阻塞引擎。
func (e *AlertEngine) fireNotify(ev AlertEvent) {
	log.Printf("[告警] %s agent=%s rule=%s value=%.1f%%", ev.Status, ev.AgentID, ev.Rule.Name, ev.Value)
	if e.notifier == nil {
		return
	}
	go func() {
		if err := e.notifier.Notify(ev); err != nil {
			log.Printf("[告警] 通知发送失败: %v", err)
		}
	}()
}

// metricValue 从 Metrics 取规则关注的指标值(百分比)。
func metricValue(m agentclient.Metrics, name string) float64 {
	switch name {
	case "cpu":
		return m.CPU.Percent
	case "memory":
		return m.Memory.Percent
	case "disk":
		return m.Disk.Percent
	}
	return 0
}

// --- Webhook Notifier 实现 ---

// WebhookNotifier 按配置类型发送到钉钉/飞书/企业微信/generic。
type WebhookNotifier struct {
	cfg WebhookConfig
}

// NewWebhookNotifier 创建 Webhook 通知器。
func NewWebhookNotifier(cfg WebhookConfig) *WebhookNotifier {
	return &WebhookNotifier{cfg: cfg}
}

// Notify 按平台格式发送告警。
func (n *WebhookNotifier) Notify(ev AlertEvent) error {
	text := formatAlertText(ev)
	body, err := n.buildBody(text)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", n.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 %d", resp.StatusCode)
	}
	return nil
}

// formatAlertText 生成通知文本。
func formatAlertText(ev AlertEvent) string {
	switch ev.Status {
	case "firing":
		return fmt.Sprintf("【deepsea 告警】%s\nAgent: %s\n指标: %s 当前 %.1f%% 超阈值 %.1f%%\n触发时间: %s",
			ev.Rule.Name, ev.AgentID, ev.Rule.Metric, ev.Value, ev.Rule.Threshold,
			ev.FiredAt.Format("2006-01-02 15:04:05"))
	case "resolved":
		return fmt.Sprintf("【deepsea 告警恢复】%s\nAgent: %s\n指标: %s 已恢复至 %.1f%%\n触发: %s\n恢复: %s",
			ev.Rule.Name, ev.AgentID, ev.Rule.Metric, ev.Value,
			ev.FiredAt.Format("2006-01-02 15:04:05"),
			ev.ResolvedAt.Format("2006-01-02 15:04:05"))
	}
	return ""
}

// buildBody 按平台构造请求体。
func (n *WebhookNotifier) buildBody(text string) ([]byte, error) {
	switch n.cfg.Type {
	case "dingtalk":
		return json.Marshal(map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": text},
		})
	case "feishu":
		return json.Marshal(map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": text},
		})
	case "wechat":
		return json.Marshal(map[string]string{
			"msgtype": "text",
			"text":    text,
		})
	default: // generic
		return json.Marshal(map[string]string{"text": text})
	}
}
