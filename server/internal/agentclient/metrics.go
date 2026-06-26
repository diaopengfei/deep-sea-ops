package agentclient

import (
	"runtime"
	"sync"
	"time"
)

// Metrics 是一次完整的资源指标采样, JSON 序列化后回传控制面。
// 字段含义参考 Prometheus node_exporter, 但轻量化(只保留运维最关心的水位)。
//
// 内存单位: 字节; 百分比: 0-100; 网络速率: 字节/秒; 负载: 1/5/15 分钟平均。
type Metrics struct {
	Timestamp int64   `json:"timestamp"` // Unix 秒
	CPU       CPU     `json:"cpu"`
	Memory    Memory  `json:"memory"`
	Disk      Disk    `json:"disk"`
	Net       Network `json:"net"`
	Load      Load    `json:"load"`
	OS        string  `json:"os"`
}

// CPU 使用率。
type CPU struct {
	Percent float64 `json:"percent"` // 总体使用率 0-100
}

// Memory 内存使用情况。
type Memory struct {
	Percent   float64 `json:"percent"`   // 使用率 0-100
	Total     uint64  `json:"total"`     // 总量(字节)
	Used      uint64  `json:"used"`      // 已用(字节)
	Available uint64  `json:"available"` // 可用(字节)
}

// Disk 根分区(或系统盘)使用情况。
type Disk struct {
	Percent float64 `json:"percent"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Path    string  `json:"path"` // 挂载点(Linux: /, Windows: C:)
}

// Network 网络吞吐(所有物理接口聚合, 不含 lo)。
type Network struct {
	RxBytesPerSec float64 `json:"rxBytesPerSec"` // 入站速率(字节/秒)
	TxBytesPerSec float64 `json:"txBytesPerSec"` // 出站速率(字节/秒)
}

// Load 系统平均负载(Linux/macOS, Windows 为 0)。
type Load struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// metricsState 维护跨次采样的状态, 用于计算 CPU 使用率和网络速率(差值/时间)。
// 进程内单例, 由 CollectMetrics 复用。仅 Linux 采集需要(Windows/macOS 暂无 CPU/网络速率)。
type metricsState struct {
	mu sync.Mutex

	// CPU: 上次 /proc/stat 的总时间片和空闲时间片
	lastCPUStat cpuStat

	// 网络: 上次累计字节数和时间
	lastNet     netCounter
	lastNetTime time.Time
}

type cpuStat struct {
	total uint64
	idle  uint64
	valid bool
}

type netCounter struct {
	rx uint64
	tx uint64
}

var metricsCollector = &metricsState{}

// CollectMetrics 采集一次完整指标。线程安全(内部加锁)。
// 跨平台: Linux 完整支持; Windows/macOS 暂返回内存/磁盘的部分指标(后续可引入 gopsutil)。
func CollectMetrics() Metrics {
	metricsCollector.mu.Lock()
	defer metricsCollector.mu.Unlock()

	m := Metrics{
		Timestamp: time.Now().Unix(),
		OS:        runtime.GOOS,
	}
	collectPlatform(&m)
	return m
}
