package agentclient

import (
	"bufio"
	"fmt"
	"sort"
	"strings"
	"gopkg.in/yaml.v3"
)

// ConfigSource 标识配置来源, 优先级从低到高
type ConfigSource string

const (
	SourceJar   ConfigSource = "jar"   // jar 内嵌配置(最低优先级)
	SourceLocal ConfigSource = "local" // 本地外部配置(中等)
	SourceNacos ConfigSource = "nacos" // Nacos 远程配置(最高)
)

// ConfigItem 是合并后的一条配置项
type ConfigItem struct {
	Key        string       `json:"key"`        // dot-notation 键, 如 spring.datasource.url
	Value      string       `json:"value"`      // 最终生效值
	Source     ConfigSource `json:"source"`      // 值来自哪个源
	Overridden bool         `json:"overridden"`  // 是否被更高优先级覆盖过
}

// EffectiveConfig 是三路配置合并后的最终结果
type EffectiveConfig struct {
	Items     []ConfigItem     `json:"items"`      // 所有生效配置项(按 key 排序)
	Overrides []OverrideRecord `json:"overrides"`  // 被覆盖的记录(哪个 key 被哪个源覆盖)

	// 三路原始配置文本(供用户回看, 前端直接展示)
	NacosRaw string `json:"nacosRaw"`
	LocalRaw string `json:"localRaw"`
	JarRaw   string `json:"jarRaw"`

	// 各源采集错误(空表示无错误)
	NacosErr string `json:"nacosErr"`
	LocalErr string `json:"localErr"`
	JarErr   string `json:"jarErr"`
}

// OverrideRecord 记录一个被覆盖的配置项
type OverrideRecord struct {
	Key     string       `json:"key"`
	From    ConfigSource `json:"from"`    // 被覆盖的源
	To      ConfigSource `json:"to"`      // 覆盖它的源
	OldVal  string       `json:"oldVal"`
	NewVal  string       `json:"newVal"`
}

// MergeConfigs 按Spring优先级合并三路配置: jar(低) < 本地 < Nacos(高)
// 返回最终生效配置 + 覆盖记录 + 原始文本。
func MergeConfigs(nacos, local, jar string) EffectiveConfig {
	ec := EffectiveConfig{
		NacosRaw: nacos,
		LocalRaw: local,
		JarRaw:   jar,
	}

	// 按优先级从低到高依次展平合并
	// 第一层: jar 内嵌配置
	jarKV := flattenConfig(jar, "jar")
	// 第二层: 本地外部配置(覆盖 jar)
	localKV := flattenConfig(local, "yml")
	// 第三层: Nacos 远程配置(覆盖本地和 jar)
	nacosKV := flattenConfig(nacos, "yml")

	// 合并: 先放 jar, 再用 local 覆盖, 再用 nacos 覆盖
	merged := map[string]ConfigItem{}
	for k, v := range jarKV {
		merged[k] = ConfigItem{Key: k, Value: v, Source: SourceJar, Overridden: false}
	}

	// local 覆盖 jar
	for k, v := range localKV {
		if existing, ok := merged[k]; ok {
			// 记录覆盖
			ec.Overrides = append(ec.Overrides, OverrideRecord{
				Key: k, From: existing.Source, To: SourceLocal, OldVal: existing.Value, NewVal: v,
			})
			merged[k] = ConfigItem{Key: k, Value: v, Source: SourceLocal, Overridden: true}
		} else {
			merged[k] = ConfigItem{Key: k, Value: v, Source: SourceLocal, Overridden: false}
		}
	}

	// nacos 覆盖 local 和 jar
	for k, v := range nacosKV {
		if existing, ok := merged[k]; ok {
			ec.Overrides = append(ec.Overrides, OverrideRecord{
				Key: k, From: existing.Source, To: SourceNacos, OldVal: existing.Value, NewVal: v,
			})
			merged[k] = ConfigItem{Key: k, Value: v, Source: SourceNacos, Overridden: true}
		} else {
			merged[k] = ConfigItem{Key: k, Value: v, Source: SourceNacos, Overridden: false}
		}
	}

	// 转成有序切片
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ec.Items = append(ec.Items, merged[k])
	}

	return ec
}

// flattenConfig 自动检测格式(yml/yaml 或 properties)并展平成 key-value map。
func flattenConfig(content, hint string) map[string]string {
	content = strings.TrimSpace(content)
	if content == "" {
		return map[string]string{}
	}
	// 如果 hint 是 "jar" 且内容看起来像 properties
	if hint == "properties" || looksLikeProperties(content) {
		return flattenProperties(content)
	}
	// 默认按 YAML 解析
	return flattenYAML(content)
}

// looksLikeProperties 粗略判断是否是 properties 格式(有 = 号)
func looksLikeProperties(content string) bool {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") && !strings.Contains(line, ": ") {
			return true
		}
	}
	return false
}

// flattenYAML 把嵌套 YAML 展平成 dot-notation keys。
// 例如 {spring: {datasource: {url: jdbc:...}}} -> spring.datasource.url = jdbc:...
func flattenYAML(content string) map[string]string {
	result := map[string]string{}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		// YAML 解析失败, 尝试按 properties 解析
		return flattenProperties(content)
	}
	if len(node.Content) == 0 {
		return result
	}
	walkYAML(node.Content[0], []string{}, result)
	return result
}

// walkYAML 递归遍历 YAML 节点, 把叶子节点的值收集到 result 里。
func walkYAML(node *yaml.Node, prefix []string, result map[string]string) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.MappingNode:
		// 键值对节点, 遍历每个 key-value
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			val := node.Content[i+1]
			newPrefix := append(prefix, key)
			walkYAML(val, newPrefix, result)
		}
	case yaml.SequenceNode:
		// 列表节点, 用 [index] 表示
		for i, item := range node.Content {
			newPrefix := append(append([]string{}, prefix...), fmt.Sprintf("[%d]", i))
			walkYAML(item, newPrefix, result)
		}
	case yaml.ScalarNode:
		// 叶子节点, 收集值
		key := strings.Join(prefix, ".")
		result[key] = node.Value
	}
}

// flattenProperties 解析 .properties 格式(key=value)。
func flattenProperties(content string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		// 支持 key=value 或 key:value
		var key, val string
		if idx := strings.Index(line, "="); idx >= 0 {
			key = strings.TrimSpace(line[:idx])
			val = strings.TrimSpace(line[idx+1:])
		} else if idx := strings.Index(line, ":"); idx >= 0 {
			key = strings.TrimSpace(line[:idx])
			val = strings.TrimSpace(line[idx+1:])
		} else {
			continue
		}
		result[key] = val
	}
	return result
}

// ExtractNacosAddr 从配置项里提取 Nacos 服务器地址。
// Spring Cloud Nacos 配置通常在 spring.cloud.nacos.config.server-addr 或 discovery.server-addr。
func ExtractNacosAddr(configs map[string]string) string {
	keys := []string{
		"spring.cloud.nacos.config.server-addr",
		"spring.cloud.nacos.discovery.server-addr",
		"spring.cloud.nacos.server-addr",
	}
	for _, k := range keys {
		if v, ok := configs[k]; ok && v != "" {
			return v
		}
	}
	return ""
}
