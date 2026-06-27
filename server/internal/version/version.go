package version

import (
	"strconv"
	"strings"
)

// Version 是平台版本号(控制面与 Agent 共享同一份源码, 故版本一致)。
// Agent 心跳上报此版本, 控制面据此判断 Agent 是否需要升级 / 版本兼容性。
const Version = "v0.6.6"

// Parse 把 "v0.6.6" 或 "0.6.6" 解析为 [3]int{0,6,6}。解析失败的段按 0 处理。
func Parse(v string) [3]int {
	var out [3]int
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(s, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

// Compare 比较 two 个语义化版本, 返回 -1/0/1。
func Compare(a, b string) int {
	pa, pb := Parse(a), Parse(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}
