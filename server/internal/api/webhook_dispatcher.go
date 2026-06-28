package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/deepsea-ops/server/internal/eventbus"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// WebhookDispatcher 订阅 EventBus, 把事件推送到匹配的 Webhook。
// 推送异步执行, 失败重试最多 3 次(指数退避)。
type WebhookDispatcher struct {
	store *store.Store
	bus   *eventbus.EventBus
}

// NewWebhookDispatcher 创建 Webhook 推送器。
func NewWebhookDispatcher(s *store.Store, bus *eventbus.EventBus) *WebhookDispatcher {
	return &WebhookDispatcher{store: s, bus: bus}
}

// Start 订阅 EventBus 并启动推送。返回停止函数。
func (d *WebhookDispatcher) Start() {
	if d.bus == nil {
		return
	}
	d.bus.Subscribe(d.handleEvent)
}

// handleEvent 是 EventBus 订阅回调。匹配订阅了该事件类型的 Webhook, 异步推送。
func (d *WebhookDispatcher) handleEvent(ev model.Event) {
	if d.store == nil {
		return
	}
	webhooks := d.store.ListWebhooks()
	for i := range webhooks {
		wh := webhooks[i]
		if !wh.Active {
			continue
		}
		if !eventMatch(wh.Events, ev.Type) {
			continue
		}
		// 异步推送, 失败重试
		go func(wh model.Webhook) {
			payload := map[string]interface{}{
				"type":      ev.Type,
				"timestamp": ev.Timestamp.Format(time.RFC3339),
				"payload":   ev.Payload,
			}
			if err := PushWebhookWithRetry(wh, payload, 3); err != nil {
				log.Printf("[Webhook] 推送到 %s(%s) 失败: %v", wh.Name, wh.URL, err)
			}
		}(wh)
	}
}

// eventMatch 判断事件类型是否匹配 Webhook 订阅列表。
// 订阅列表为空表示订阅全部事件。
func eventMatch(subscribed []string, eventType string) bool {
	if len(subscribed) == 0 {
		return true
	}
	for _, e := range subscribed {
		if e == eventType {
			return true
		}
	}
	return false
}

// PushWebhook 同步推送一次事件到 Webhook, 不重试。
// 用于测试推送和重试内部调用。
func PushWebhook(wh model.Webhook, payload interface{}) error {
	return PushWebhookWithRetry(wh, payload, 1)
}

// PushWebhookWithRetry 推送事件到 Webhook, 失败时按指数退避重试。
// maxRetries 为总尝试次数(含首次)。Secret 非空时带 HMAC-SHA256 签名头。
func PushWebhookWithRetry(wh model.Webhook, payload interface{}, maxRetries int) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化负载失败: %w", err)
	}
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := doPost(wh, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxRetries {
			// 指数退避: 1s, 2s, 4s...
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return lastErr
}

// doPost 执行单次 HTTP POST, 带 HMAC 签名头(若 Secret 非空)。
func doPost(wh model.Webhook, body []byte) error {
	req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "deepsea-ops-webhook/1.0")
	// HMAC-SHA256 签名: 用 Secret 对 body 做 HMAC, 接收方用同 Secret 验签
	if wh.Secret != "" {
		mac := hmac.New(sha256.New, []byte(wh.Secret))
		mac.Write(body)
		req.Header.Set("X-Deepsea-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
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
