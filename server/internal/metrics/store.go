// Package metrics 提供资源指标的内存环形缓冲存储。
//
// 设计要点(参考 Prometheus, 但轻量化, 不引入时序数据库):
//   - 每个 Agent 一个 RingBuffer, 固定容量, 满后覆盖最旧(环形)
//   - 数据只在内存, 不进 Raft(弱一致, 重启丢失, 符合"近期指标"语义)
//   - 同时维护最新一条, 供实时卡片查询
//
// 数据来源:
//   - 心跳的 CPU/内存(轻量, 每 5s) → SetLatest
//   - 定时 COLLECT_METRICS 完整指标(每 30s) → Record(进环形缓冲)
package metrics

import (
	"sync"
	"time"

	"github.com/deepsea-ops/server/internal/agentclient"
)

// Sample 是环形缓冲中的一个采样点(完整指标 + 采集时间)。
type Sample struct {
	Time     time.Time `json:"time"`
	Metrics  agentclient.Metrics `json:"metrics"`
}

// DefaultCapacity 默认环形缓冲容量: 30s 采样 × 240 = 2 小时历史。
const DefaultCapacity = 240

// RingBuffer 是单个 Agent 的环形缓冲。固定容量, 满后覆盖最旧。
type RingBuffer struct {
	mu       sync.RWMutex
	samples  []Sample
	capacity int
	head     int // 下一个写入位置
	count    int // 已有数量(<= capacity)
	latest   *Sample
}

// NewRingBuffer 创建指定容量的环形缓冲。
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &RingBuffer{
		samples:  make([]Sample, capacity),
		capacity: capacity,
	}
}

// Record 追加一个采样点。满后覆盖最旧。
func (rb *RingBuffer) Record(m agentclient.Metrics) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	s := Sample{Time: time.Now(), Metrics: m}
	rb.samples[rb.head] = s
	rb.head = (rb.head + 1) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	}
	latest := s
	rb.latest = &latest
}

// SetLatest 只更新最新值(不入缓冲), 用于心跳的轻量 CPU/内存刷新。
func (rb *RingBuffer) SetLatest(cpu, mem float64) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.latest == nil {
		s := Sample{Time: time.Now()}
		s.Metrics.CPU.Percent = cpu
		s.Metrics.Memory.Percent = mem
		rb.latest = &s
	} else {
		// 复制最新点, 仅更新 CPU/内存和时间, 保留采集器写入的完整字段
		cp := *rb.latest
		cp.Time = time.Now()
		cp.Metrics.CPU.Percent = cpu
		cp.Metrics.Memory.Percent = mem
		rb.latest = &cp
	}
}

// Latest 返回最新一条采样点(可能来自心跳或定时采集)。
func (rb *RingBuffer) Latest() *Sample {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.latest == nil {
		return nil
	}
	cp := *rb.latest
	return &cp
}

// History 返回按时间升序的历史采样点(最多 capacity 条)。
func (rb *RingBuffer) History() []Sample {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.count == 0 {
		return []Sample{}
	}
	out := make([]Sample, 0, rb.count)
	// head 指向最旧(若已满)或第一个有效位置(若未满)
	start := 0
	if rb.count == rb.capacity {
		start = rb.head // head 即最旧
	} else {
		start = 0
	}
	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.capacity
		out = append(out, rb.samples[idx])
	}
	return out
}

// Store 管理所有 Agent 的环形缓冲。
// 仅 Leader 节点持有有效数据(Agent 只连 Leader), Follower 上为空。
type Store struct {
	mu       sync.RWMutex
	buffers  map[string]*RingBuffer
	capacity int
}

// NewStore 创建指标存储。capacity 为每个 Agent 的环形缓冲容量。
func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{
		buffers:  make(map[string]*RingBuffer),
		capacity: capacity,
	}
}

// getOrCreate 获取(或创建)指定 Agent 的缓冲。调用方需持有读锁或确保并发安全。
func (s *Store) getOrCreate(agentID string) *RingBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	rb, ok := s.buffers[agentID]
	if !ok {
		rb = NewRingBuffer(s.capacity)
		s.buffers[agentID] = rb
	}
	return rb
}

// Record 记录一条完整指标(来自定时 COLLECT_METRICS)。
func (s *Store) Record(agentID string, m agentclient.Metrics) {
	s.getOrCreate(agentID).Record(m)
}

// SetLatest 更新最新 CPU/内存(来自心跳, 轻量)。
func (s *Store) SetLatest(agentID string, cpu, mem float64) {
	s.getOrCreate(agentID).SetLatest(cpu, mem)
}

// Latest 返回指定 Agent 的最新采样点; 无数据返回 nil。
func (s *Store) Latest(agentID string) *Sample {
	s.mu.RLock()
	rb, ok := s.buffers[agentID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	return rb.Latest()
}

// History 返回指定 Agent 的历史采样点(按时间升序)。
func (s *Store) History(agentID string) []Sample {
	s.mu.RLock()
	rb, ok := s.buffers[agentID]
	s.mu.RUnlock()
	if !ok {
		return []Sample{}
	}
	return rb.History()
}

// RemoveAgent 移除指定 Agent 的缓冲(Agent 断开时调用, 避免内存泄漏)。
func (s *Store) RemoveAgent(agentID string) {
	s.mu.Lock()
	delete(s.buffers, agentID)
	s.mu.Unlock()
}

// Agents 返回有指标数据的 Agent ID 列表。
func (s *Store) Agents() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.buffers))
	for id := range s.buffers {
		out = append(out, id)
	}
	return out
}
