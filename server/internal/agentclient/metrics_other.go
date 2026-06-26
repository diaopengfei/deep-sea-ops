//go:build !linux

package agentclient

// 非 Linux 平台(Windows/macOS)的指标采集 stub。
// 完整跨平台采集建议后续引入 github.com/shirou/gopsutil,
// 当前仅 Linux 生产环境使用, Windows/macOS 主要用于开发联调, 指标返回零值。
func collectPlatform(m *Metrics) {
	// no-op: 非 Linux 平台暂不采集, 字段保持零值
}
