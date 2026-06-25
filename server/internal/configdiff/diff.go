package configdiff

import (
	"encoding/json"
	"sort"
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
//
// v0.6.2 起新增 Semantic 字段: 按 key 对比三路配置的值, 展示 OldVal→NewVal 的变化,
// 直接回答运维最关心的"同一配置项在不同来源的值差异"。
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

	// Semantic 是按 key 对比的值级差异(v0.6.2), 每项描述一个 key 在三路中的值与是否一致。
	Semantic []KeyDiff `json:"semantic,omitempty"`
}

// KeyDiff 描述单个配置 key 在三路配置中的值差异。
// 运维场景最关心: 同一 key 在 Nacos/本地/jar 里的值是否一致, 不一致时各是什么。
type KeyDiff struct {
	Key     string `json:"key"`
	Nacos   string `json:"nacos,omitempty"`   // Nacos 中的值, 没有则空
	Local   string `json:"local,omitempty"`   // 本地配置文件中的值, 没有则空
	Jar     string `json:"jar,omitempty"`     // jar 包内配置的值, 没有则空
	Consistent bool `json:"consistent"`   // 三路值是否完全一致
}

// Compare 接收三个来源的配置文本, 生成 diff 报告。
// 比对策略: 按行拆分, 用"行内容 + 出现的来源集合"做分组。
// 这不是 Myers 算法(不做顺序敏感的最小编辑距离), 而是集合差异,
// 对配置比对场景足够(配置项的顺序差异通常不重要, 内容差异才重要)。
//
// v0.6.2 起同时生成 Semantic: 把三路配置按 key=value 展平后对比值,
// 直接展示"同一 key 在三路中的值是否一致"。
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

	// 语义级 diff: 按 key=value 展平后对比值
	r.Semantic = compareSemantic(nacos, local, jar)
	return r
}

// compareSemantic 把三路配置按 key=value 展平, 对比每个 key 的值。
// 展平逻辑: 识别 properties 风格(key=value 或 key:value) 和 YAML 风格(key: value),
// 只提取顶层 key(嵌套用点号拼接的 key 作为整体匹配, 不递归展开, 保持简单)。
// 一致(三路值相同)的 key 也会包含在结果中, 供前端展示完整配置视图。
func compareSemantic(nacos, local, jar string) []KeyDiff {
	nacosKV := flattenConfig(nacos)
	localKV := flattenConfig(local)
	jarKV := flattenConfig(jar)

	// 收集所有 key(按字母序)
	allKeys := make(map[string]struct{})
	for k := range nacosKV {
		allKeys[k] = struct{}{}
	}
	for k := range localKV {
		allKeys[k] = struct{}{}
	}
	for k := range jarKV {
		allKeys[k] = struct{}{}
	}

	diffs := make([]KeyDiff, 0, len(allKeys))
	for k := range allKeys {
		nv, lok := nacosKV[k]
		lv, lok2 := localKV[k]
		jv, jok := jarKV[k]

		// 三路都没有(理论不会), 跳过
		if !lok && !lok2 && !jok {
			continue
		}

		consistent := lok && lok2 && jok && nv == lv && lv == jv
		diffs = append(diffs, KeyDiff{
			Key:        k,
			Nacos:      nv,
			Local:      lv,
			Jar:        jv,
			Consistent: consistent,
		})
	}

	// 按 key 排序, 方便前端展示
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Key < diffs[j].Key
	})
	return diffs
}

// flattenConfig 把配置文本按 key=value 展平成 map。
// 支持 properties(key=value / key:value, # 或 ! 注释) 和 YAML 顶层(key: value, # 注释)。
// 嵌套结构的 key 不递归展开, 用整行 key 作为匹配单位, 保持简单可靠。
func flattenConfig(s string) map[string]string {
	kv := make(map[string]string)
	lines := splitLines(s)
	for _, line := range lines {
		if line == "" {
			continue
		}
		// 跳过注释
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		// properties: key=value 或 key:value
		if idx := strings.IndexAny(line, "=:"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" {
				kv[key] = val
			}
		}
	}
	return kv
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