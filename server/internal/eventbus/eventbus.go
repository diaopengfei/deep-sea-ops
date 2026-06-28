// Package eventbus 实现轻量异步事件总线(v0.7.0 API 开放与集成)。
//
// 设计目标:
//   - 发布非阻塞: Publish 把事件投递到缓冲通道立即返回, 不等待订阅者处理。
//   - 多订阅者: 每个订阅者独立 goroutine 消费, 互不影响。
//   - 失败隔离: 某个订阅者 panic 或慢消费不影响其他订阅者和发布方。
//   - 退出可控: Stop 通知所有订阅者退出, 关闭通道前投递的事件会被消费完。
//
// 适用场景: Webhook 推送、审计扩展、_metrics 派生等。
// 不适用场景: 强一致同步事件(请用直接函数调用)。
package eventbus

import (
	"log"
	"sync"

	"github.com/deepsea-ops/server/internal/model"
)

// Handler 是事件订阅回调。
type Handler func(event model.Event)

// EventBus 是事件总线核心。
// 发布者调 Publish 把事件投递到内部通道; 订阅者通过 Subscribe 注册回调,
// 总线为每个回调启动独立 goroutine 从通道读取并调用。
type EventBus struct {
	ch       chan model.Event
	handlers []Handler
	mu       sync.RWMutex
	stopCh   chan struct{}
	stopped  bool
	wg       sync.WaitGroup
}

// New 创建事件总线。bufferSize 为内部通道缓冲大小(建议 256)。
// 缓冲满时 Publish 会阻塞(背压), 避免事件无限堆积。
func New(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &EventBus{
		ch:     make(chan model.Event, bufferSize),
		stopCh: make(chan struct{}),
	}
}

// Start 启动事件分发 goroutine。
// 建议在所有 Subscribe 后、Publish 前调用, 避免 Start 到 Subscribe 窗口期内发布的事件无订阅者而丢失。
// Subscribe 内部用互斥锁保护, Start 后再 Subscribe 也能安全生效, 但仍推荐先订阅再启动。
func (b *EventBus) Start() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case ev, ok := <-b.ch:
				if !ok {
					// 通道已关闭, 排空后退出
					return
				}
				b.dispatch(ev)
			case <-b.stopCh:
				// 收到停止信号, 排空剩余事件后退出
				for {
					select {
					case ev := <-b.ch:
						b.dispatch(ev)
					default:
						return
					}
				}
			}
		}
	}()
}

// Stop 停止事件总线。已投递但未处理的事件会被消费完。
// 可安全多次调用。
func (b *EventBus) Stop() {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.stopped = true
	close(b.stopCh)
	b.mu.Unlock()
	b.wg.Wait()
}

// Subscribe 注册一个事件订阅者。返回取消订阅函数。
// 内部用互斥锁保护, Start 前后均可调用(并发安全); 但推荐在 Start 前订阅,
// 避免 Start 到 Subscribe 窗口期内发布的事件无订阅者接收。
func (b *EventBus) Subscribe(h Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := len(b.handlers)
	b.handlers = append(b.handlers, h)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if idx < len(b.handlers) {
			b.handlers[idx] = nil // 置空, 避免切片元素移动影响其他订阅者的取消函数
		}
	}
}

// Publish 发布一个事件。非阻塞投递到通道, 由后台 goroutine 分发。
// 通道满时阻塞(背压), 避免事件无限堆积导致 OOM。
func (b *EventBus) Publish(ev model.Event) {
	b.mu.RLock()
	stopped := b.stopped
	b.mu.RUnlock()
	if stopped {
		return
	}
	select {
	case b.ch <- ev:
	case <-b.stopCh:
	}
}

// dispatch 把事件投递给所有订阅者。单个订阅者 panic 不影响其他订阅者。
func (b *EventBus) dispatch(ev model.Event) {
	b.mu.RLock()
	handlers := make([]Handler, 0, len(b.handlers))
	for _, h := range b.handlers {
		if h != nil {
			handlers = append(handlers, h)
		}
	}
	b.mu.RUnlock()
	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[事件总线] 订阅者 panic: %v", r)
				}
			}()
			h(ev)
		}()
	}
}
