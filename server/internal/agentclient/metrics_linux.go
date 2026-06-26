//go:build linux

package agentclient

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func collectPlatform(m *Metrics) {
	collectLinuxCPU(m)
	collectLinuxMem(m)
	collectLinuxDisk(m)
	collectLinuxNet(m)
	collectLinuxLoad(m)
}

// collectLinuxCPU 读 /proc/stat 首行, 与上次差值计算使用率。
// /proc/stat 首行: cpu user nice system idle iowait irq softirq steal guest guest_nice
func collectLinuxCPU(m *Metrics) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return
		}
		var total, idle uint64
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 { // idle 是第 4 个数(fields[4])
				idle = v
			}
		}
		cur := cpuStat{total: total, idle: idle, valid: true}
		prev := metricsCollector.lastCPUStat
		metricsCollector.lastCPUStat = cur
		if prev.valid && cur.total > prev.total {
			totalDelta := cur.total - prev.total
			idleDelta := cur.idle - prev.idle
			if totalDelta > 0 {
				m.CPU.Percent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
			}
		}
		return
	}
}

// collectLinuxMem 读 /proc/meminfo。
func collectLinuxMem(m *Metrics) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	mem := make(map[string]uint64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, " kB")
		val, _ := strconv.ParseUint(valStr, 10, 64)
		mem[key] = val * 1024 // kB -> 字节
	}
	m.Memory.Total = mem["MemTotal"]
	m.Memory.Available = mem["MemAvailable"]
	if m.Memory.Available == 0 {
		m.Memory.Available = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	m.Memory.Used = m.Memory.Total - m.Memory.Available
	if m.Memory.Total > 0 {
		m.Memory.Percent = float64(m.Memory.Used) / float64(m.Memory.Total) * 100
	}
}

// collectLinuxDisk 用 syscall.Statfs 取根分区使用情况。
func collectLinuxDisk(m *Metrics) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return
	}
	m.Disk.Path = "/"
	m.Disk.Total = stat.Blocks * uint64(stat.Bsize)
	m.Disk.Free = stat.Bfree * uint64(stat.Bsize)
	m.Disk.Used = m.Disk.Total - m.Disk.Free
	if m.Disk.Total > 0 {
		m.Disk.Percent = float64(m.Disk.Used) / float64(m.Disk.Total) * 100
	}
}

// collectLinuxNet 读 /proc/net/dev, 聚合非 lo 接口的收发字节, 与上次差值算速率。
func collectLinuxNet(m *Metrics) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()
	var rx, tx uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}

	now := time.Now()
	cur := netCounter{rx: rx, tx: tx}
	prev := metricsCollector.lastNet
	prevTime := metricsCollector.lastNetTime
	metricsCollector.lastNet = cur
	metricsCollector.lastNetTime = now

	if !prevTime.IsZero() {
		elapsed := now.Sub(prevTime).Seconds()
		if elapsed > 0 {
			m.Net.RxBytesPerSec = float64(cur.rx-prev.rx) / elapsed
			m.Net.TxBytesPerSec = float64(cur.tx-prev.tx) / elapsed
		}
	}
}

// collectLinuxLoad 读 /proc/loadavg。
func collectLinuxLoad(m *Metrics) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return
	}
	m.Load.Load1, _ = strconv.ParseFloat(fields[0], 64)
	m.Load.Load5, _ = strconv.ParseFloat(fields[1], 64)
	m.Load.Load15, _ = strconv.ParseFloat(fields[2], 64)
}
