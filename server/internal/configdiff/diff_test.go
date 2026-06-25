// configdiff 包单元测试: 验证三路配置比对的行级与语义级差异生成。
// 重点是 v0.6.2 新增的语义级 diff(compareSemantic/flattenConfig)。
package configdiff

import (
	"testing"
)

// --- flattenConfig: key=value 展平 ---

func TestFlattenConfig_PropertiesStyle(t *testing.T) {
	input := `
# 注释行
server.port=8080
db.host: localhost
db.port = 3306
! 感叹号注释

empty.key=
`
	kv := flattenConfig(input)
	cases := map[string]string{
		"server.port": "8080",
		"db.host":     "localhost",
		"db.port":     "3306",
		"empty.key":   "",
	}
	for k, want := range cases {
		if got := kv[k]; got != want {
			t.Errorf("flattenConfig[%q] = %q, want %q", k, got, want)
		}
	}
	if _, ok := kv["# 注释行"]; ok {
		t.Error("注释行不应被解析为 key")
	}
	if _, ok := kv["! 感叹号注释"]; ok {
		t.Error("感叹号注释不应被解析为 key")
	}
	if _, ok := kv[""]; ok {
		t.Error("空行不应产生 key")
	}
}

func TestFlattenConfig_EmptyAndComments(t *testing.T) {
	kv := flattenConfig("")
	if len(kv) != 0 {
		t.Errorf("空配置应返回空 map, got %v", kv)
	}
	kv = flattenConfig("# only comment\n# another")
	if len(kv) != 0 {
		t.Errorf("纯注释应返回空 map, got %v", kv)
	}
}

// --- compareSemantic: 语义级差异 ---

func TestCompareSemantic_AllConsistent(t *testing.T) {
	cfg := "name=app\nport=8080\n"
	diffs := compareSemantic(cfg, cfg, cfg)
	if len(diffs) != 2 {
		t.Fatalf("应返回 2 个 key, got %d", len(diffs))
	}
	for _, d := range diffs {
		if !d.Consistent {
			t.Errorf("key %q 应一致", d.Key)
		}
	}
}

func TestCompareSemantic_ValueMismatch(t *testing.T) {
	nacos := "port=8080\nhost=nacos-host"
	local := "port=8080\nhost=local-host"
	jar := "port=8080\nhost=nacos-host"
	diffs := compareSemantic(nacos, local, jar)

	// 找到 host 的差异
	var hostDiff *KeyDiff
	for i := range diffs {
		if diffs[i].Key == "host" {
			hostDiff = &diffs[i]
			break
		}
	}
	if hostDiff == nil {
		t.Fatal("未找到 host key")
	}
	if hostDiff.Consistent {
		t.Error("host 值不同, 不应标记为一致")
	}
	if hostDiff.Nacos != "nacos-host" || hostDiff.Local != "local-host" || hostDiff.Jar != "nacos-host" {
		t.Errorf("host 值不正确: nacos=%q local=%q jar=%q", hostDiff.Nacos, hostDiff.Local, hostDiff.Jar)
	}

	// port 三方一致
	var portDiff *KeyDiff
	for i := range diffs {
		if diffs[i].Key == "port" {
			portDiff = &diffs[i]
			break
		}
	}
	if portDiff == nil || !portDiff.Consistent {
		t.Error("port 三方一致, 应标记 consistent")
	}
}

func TestCompareSemantic_KeyMissingInSomeSource(t *testing.T) {
	nacos := "shared=1\nonly-nacos=2"
	local := "shared=1\nonly-local=3"
	jar := "shared=1"
	diffs := compareSemantic(nacos, local, jar)

	byKey := make(map[string]KeyDiff, len(diffs))
	for _, d := range diffs {
		byKey[d.Key] = d
	}

	// shared 三方都有且一致
	if d, ok := byKey["shared"]; !ok || !d.Consistent {
		t.Errorf("shared 应三方一致: %+v", d)
	}

	// only-nacos 仅 nacos 有, 不一致
	if d, ok := byKey["only-nacos"]; !ok {
		t.Error("缺少 only-nacos key")
	} else {
		if d.Consistent {
			t.Error("only-nacos 仅 nacos 有, 不应一致")
		}
		if d.Nacos != "2" || d.Local != "" || d.Jar != "" {
			t.Errorf("only-nacos 值错误: %+v", d)
		}
	}

	// only-local 仅 local 有
	if d, ok := byKey["only-local"]; !ok {
		t.Error("缺少 only-local key")
	} else if d.Local != "3" || d.Nacos != "" {
		t.Errorf("only-local 值错误: %+v", d)
	}
}

func TestCompareSemantic_SortedByKey(t *testing.T) {
	cfg := "zebra=1\napple=2\nmango=3"
	diffs := compareSemantic(cfg, cfg, cfg)
	if len(diffs) != 3 {
		t.Fatalf("应返回 3 个 key, got %d", len(diffs))
	}
	want := []string{"apple", "mango", "zebra"}
	for i, w := range want {
		if diffs[i].Key != w {
			t.Errorf("第 %d 个 key = %q, want %q (应按字母序)", i, diffs[i].Key, w)
		}
	}
}

// --- Compare: 整合行级 + 语义级 ---

func TestCompare_IntegratesSemantic(t *testing.T) {
	nacos := "port=8080\n"
	local := "port=9090\n"
	jar := "port=8080\n"
	r := Compare(nacos, local, jar)

	// 语义级差异应存在且 port 不一致
	if len(r.Semantic) == 0 {
		t.Fatal("Semantic 应非空")
	}
	var portDiff *KeyDiff
	for i := range r.Semantic {
		if r.Semantic[i].Key == "port" {
			portDiff = &r.Semantic[i]
			break
		}
	}
	if portDiff == nil {
		t.Fatal("Semantic 中缺少 port")
	}
	if portDiff.Consistent {
		t.Error("port 三方值不同, 不应一致")
	}
	if portDiff.Nacos != "8080" || portDiff.Local != "9090" || portDiff.Jar != "8080" {
		t.Errorf("port 值错误: %+v", portDiff)
	}

	// 行级差异也应存在(port=8080 和 port=9090 是不同行)
	if len(r.Consistent) != 0 {
		t.Errorf("不应有三方一致的行, got %v", r.Consistent)
	}
}

// --- BuildReport: 从快照 JSON 构建 ---

func TestBuildReport_ParsesSnapshot(t *testing.T) {
	// 构造一个 ConfigSnapshot 的 JSON
	snap := `{"nacos":"k=v\n","local":"k=v\n","jar":"k=v\n","nacosErr":"","localErr":"","jarErr":""}`
	r := BuildReport(snap)
	if r.NacosErr != "" || r.LocalErr != "" || r.JarErr != "" {
		t.Errorf("不应有采集错误: %+v", r)
	}
	if len(r.Semantic) != 1 || !r.Semantic[0].Consistent {
		t.Errorf("应有一个一致 key: %+v", r.Semantic)
	}
}

func TestBuildReport_CollectionError(t *testing.T) {
	snap := `{"nacos":"","local":"","jar":"","nacosErr":"connection refused","localErr":"","jarErr":""}`
	r := BuildReport(snap)
	if r.NacosErr != "connection refused" {
		t.Errorf("NacosErr = %q, want 'connection refused'", r.NacosErr)
	}
}

func TestBuildReport_InvalidJSON(t *testing.T) {
	r := BuildReport("{not json")
	if r.NacosErr == "" {
		t.Error("非法 JSON 应在 NacosErr 中返回解析错误")
	}
}
