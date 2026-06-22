package configdiff

import (
	"encoding/json"
	"strings"
)

// Side 是配置来源的标识, 用于 diff 报告里标注差异属于哪一侧。
type Side string

const (
	SideNacos Side = "nacos"
	SideLocal Side = "local"
	SideJar   Side = "jar"
)

// LineDiff 描述一行配置在某个来源的状态。
type LineDiff struct {
	Line   string `json:"line"`   // 配置行内容
	Sides  []Side `json:"sides"`  // 这行出现在哪些来源里
}

// DiffReport 是三路配置比对的完整报告。
// 把三个来源的配置按行拆开, 找出哪些行是三个都有(一致)、哪些是部分缺失。
type DiffReport struct {
	NacosErr string `json:"nacosErr,omitempty"`
	LocalErr string `json:"localErr,omitempty"`
	JarErr   string `json:"jarErr,omitempty"`

	Consistent   []string `json:"consistent"`    // 三方一致的行
	OnlyNacos    []string `json:"onlyNacos"`     // 仅 Nacos 有
	OnlyLocal    []string `json:"onlyLocal"`     // 仅本地有
	OnlyJar      []string `json:"onlyJar"`       // 仅 jar 有
	NacosLocal   []string `json:"nacosLocal"`    // Nacos 和本地有, jar 没有
	NacosJar     []string `json:"nacosJar"`      // Nacos 和 jar 有, 本地没有
	LocalJar     []string `json:"localJar"`     // 本地和 jar 有, Nacos 没有
}

// Compare 接收三个来源的配置文本, 生成 diff 报告。
// 比对策略: 按行拆分, 用"行内容 + 出现的来源集合"做分组。
// 这不是 Myers 算法(不做顺序敏感的最小编辑距离), 而是集合差异,
// 对配置比对场景足够(配置项的顺序差异通常不重要, 内容差异才重要)。
func Compare(nacos, local, jar string) DiffReport {
	r := DiffReport{}

	// 按行拆分, 去除空行和首尾空白
	nacosLines := splitLines(nacos)
	localLines := splitLines(local)
	jarLines := splitLines(jar)

	// 用 map 记录每行出现在哪些来源: line -> {nacos?, local?, jar?}
	type presence struct {
		nacos, local, jar bool
	}
	seen := make(map[string]*presence)

	add := func(lines []string, set func(p *presence)) {
		for _, line := range lines {
			if line == "" {
				continue
			}
			p, ok := seen[line]
			if !ok {
				p = &presence{}
				seen[line] = p
			}
			set(p)
		}
	}
	add(nacosLines, func(p *presence) { p.nacos = true })
	add(localLines, func(p *presence) { p.local = true })
	add(jarLines, func(p *presence) { p.jar = true })

	// 按出现组合分类
	for line, p := range seen {
		switch {
		case p.nacos && p.local && p.jar:
			r.Consistent = append(r.Consistent, line)
		case p.nacos && !p.local && !p.jar:
			r.OnlyNacos = append(r.OnlyNacos, line)
		case !p.nacos && p.local && !p.jar:
			r.OnlyLocal = append(r.OnlyLocal, line)
		case !p.nacos && !p.local && p.jar:
			r.OnlyJar = append(r.OnlyJar, line)
		case p.nacos && p.local && !p.jar:
			r.NacosLocal = append(r.NacosLocal, line)
		case p.nacos && !p.local && p.jar:
			r.NacosJar = append(r.NacosJar, line)
		case !p.nacos && p.local && p.jar:
			r.LocalJar = append(r.LocalJar, line)
		}
	}
	return r
}

// splitLines 按换行拆分, 去首尾空白和空行。
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		out = append(out, p)
	}
	return out
}
// BuildReport 从 Agent 回传的 JSON 快照解析出三份配置, 调 Compare 生成 diff 报告。
// snapJSON 是 Agent 端 CollectConfigs 产出的 ConfigSnapshot 的 JSON。
// 它同时把采集错误透传到报告里, 前端可以据此提示哪个源采集失败。
func BuildReport(snapJSON string) DiffReport {
	var snap struct {
		Nacos    string `json:"nacos"`
		Local    string `json:"local"`
		Jar      string `json:"jar"`
		NacosErr string `json:"nacosErr"`
		LocalErr string `json:"localErr"`
		JarErr   string `json:"jarErr"`
	}
	if err := json.Unmarshal([]byte(snapJSON), &snap); err != nil {
		return DiffReport{NacosErr: "解析采集结果失败: " + err.Error()}
	}
	r := Compare(snap.Nacos, snap.Local, snap.Jar)
	r.NacosErr = snap.NacosErr
	r.LocalErr = snap.LocalErr
	r.JarErr = snap.JarErr
	return r
}