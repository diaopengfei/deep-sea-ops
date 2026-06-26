package metrics

import (
	"testing"

	"github.com/deepsea-ops/server/internal/agentclient"
)

func TestRingBuffer_RecordAndHistory(t *testing.T) {
	rb := NewRingBuffer(3)
	// 空缓冲
	if got := rb.History(); len(got) != 0 {
		t.Fatalf("空缓冲 History 长度 = %d, want 0", len(got))
	}
	if rb.Latest() != nil {
		t.Fatal("空缓冲 Latest 应为 nil")
	}

	// 写入 2 条
	rb.Record(agentclient.Metrics{CPU: agentclient.CPU{Percent: 10}})
	rb.Record(agentclient.Metrics{CPU: agentclient.CPU{Percent: 20}})
	hist := rb.History()
	if len(hist) != 2 {
		t.Fatalf("History 长度 = %d, want 2", len(hist))
	}
	// 升序
	if hist[0].Metrics.CPU.Percent != 10 || hist[1].Metrics.CPU.Percent != 20 {
		t.Errorf("顺序错误: got %v, %v", hist[0].Metrics.CPU.Percent, hist[1].Metrics.CPU.Percent)
	}
	// Latest 是最新
	if rb.Latest().Metrics.CPU.Percent != 20 {
		t.Errorf("Latest = %v, want 20", rb.Latest().Metrics.CPU.Percent)
	}
}

func TestRingBuffer_OverwriteOldest(t *testing.T) {
	rb := NewRingBuffer(3)
	for i := 1; i <= 5; i++ {
		rb.Record(agentclient.Metrics{CPU: agentclient.CPU{Percent: float64(i)}})
	}
	hist := rb.History()
	// 容量 3, 写入 5, 应只保留最近 3 条(3,4,5)
	if len(hist) != 3 {
		t.Fatalf("History 长度 = %d, want 3", len(hist))
	}
	want := []float64{3, 4, 5}
	for i, w := range want {
		if hist[i].Metrics.CPU.Percent != w {
			t.Errorf("hist[%d] = %v, want %v", i, hist[i].Metrics.CPU.Percent, w)
		}
	}
}

func TestRingBuffer_SetLatestMergesHeartbeat(t *testing.T) {
	rb := NewRingBuffer(10)
	// 先 Record 一条完整指标
	rb.Record(agentclient.Metrics{
		CPU:    agentclient.CPU{Percent: 10},
		Memory: agentclient.Memory{Percent: 20, Total: 1000},
	})
	// 心跳只更新 CPU/内存
	rb.SetLatest(55, 66)
	latest := rb.Latest()
	if latest.Metrics.CPU.Percent != 55 {
		t.Errorf("心跳后 CPU = %v, want 55", latest.Metrics.CPU.Percent)
	}
	if latest.Metrics.Memory.Percent != 66 {
		t.Errorf("心跳后内存 = %v, want 66", latest.Metrics.Memory.Percent)
	}
	// 采集的完整字段应保留(Memory.Total)
	if latest.Metrics.Memory.Total != 1000 {
		t.Errorf("心跳不应覆盖 Memory.Total = %v, want 1000", latest.Metrics.Memory.Total)
	}
	// SetLatest 不入缓冲
	if len(rb.History()) != 1 {
		t.Errorf("SetLatest 不应入缓冲, History 长度 = %d, want 1", len(rb.History()))
	}
}

func TestStore_GetOrCreate(t *testing.T) {
	s := NewStore(5)
	// 不存在的 Agent
	if s.Latest("a1") != nil {
		t.Fatal("不存在的 Agent Latest 应为 nil")
	}
	if got := s.History("a1"); len(got) != 0 {
		t.Fatalf("不存在的 Agent History 长度 = %d, want 0", len(got))
	}

	// 记录后可查
	s.Record("a1", agentclient.Metrics{CPU: agentclient.CPU{Percent: 42}})
	if s.Latest("a1") == nil {
		t.Fatal("记录后 Latest 不应为 nil")
	}
	if s.Latest("a1").Metrics.CPU.Percent != 42 {
		t.Errorf("Latest CPU = %v, want 42", s.Latest("a1").Metrics.CPU.Percent)
	}

	// SetLatest
	s.SetLatest("a2", 30, 40)
	if s.Latest("a2").Metrics.CPU.Percent != 30 {
		t.Errorf("a2 Latest CPU = %v, want 30", s.Latest("a2").Metrics.CPU.Percent)
	}

	// Agents 列表
	agents := s.Agents()
	if len(agents) != 2 {
		t.Errorf("Agents 长度 = %d, want 2", len(agents))
	}

	// RemoveAgent
	s.RemoveAgent("a1")
	if s.Latest("a1") != nil {
		t.Fatal("移除后 Latest 应为 nil")
	}
}

func TestStore_DefaultCapacity(t *testing.T) {
	s := NewStore(0) // 0 应回退到默认值
	rb := s.getOrCreate("a")
	for i := 0; i < DefaultCapacity+10; i++ {
		rb.Record(agentclient.Metrics{})
	}
	if len(rb.History()) != DefaultCapacity {
		t.Errorf("默认容量 History 长度 = %d, want %d", len(rb.History()), DefaultCapacity)
	}
}
