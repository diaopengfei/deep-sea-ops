package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/deepsea-ops/server/internal/eventbus"
	"github.com/deepsea-ops/server/internal/model"
	"github.com/deepsea-ops/server/internal/store"
)

// webhookHTTPClient 是 Webhook 推送共享的 HTTP 客户端, 复用连接池降低 TCP/TLS 握手开销。
var webhookHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// WebhookDispatcher 订阅 EventBus, 把事件推送到匹配的 Webhook。
// 推送异步执行, 失败重试最多 3 次(指数退避)。
type WebhookDispatcher struct {
	store *store.Store
	bus   *eventbus.EventBus
	wg    sync.WaitGroup // 跟踪在途推送 goroutine, Stop 时等待完成
}

// NewWebhookDispatcher 创建 Webhook 推送器。
func NewWebhookDispatcher(s *store.Store, bus *eventbus.EventBus) *WebhookDispatcher {
	return &WebhookDispatcher{store: s, bus: bus}
}

// Start 订阅 EventBus。必须在 eventBus.Start() 之前调用, 避免启动窗口事件丢失。
func (d *WebhookDispatcher) Start() {
	if d.bus == nil {
		return
	}
	d.bus.Subscribe(d.handleEvent)
}

// Stop 等待所有在途推送 goroutine 完成。
// 应在 eventBus.Stop() 之前调用(LIFO defer 顺序), 保证事件已排空后再等推送完成。
func (d *WebhookDispatcher) Stop() {
	d.wg.Wait()
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
		// 异步推送, 失败重试。用 WaitGroup 跟踪以便优雅关闭。
		d.wg.Add(1)
		go func(wh model.Webhook) {
			defer d.wg.Done()
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
// 使用共享 webhookHTTPClient 复用连接池; 读完并丢弃 response body 以保持 keepalive。
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
	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return err
	}
	// 读完 body 再 Close, 否则连接无法复用(keepalive 失效)
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 %d", resp.StatusCode)
	}
	return nil
}
