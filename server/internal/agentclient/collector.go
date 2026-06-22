package agentclient

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ConfigSources 描述一次配置采集的输入: 三类配置源。
// 控制面下发 COLLECT_CONFIGS 指令时, 把这些信息放在 params 里。
type ConfigSources struct {
	// Nacos 配置: dataId + group + namespace + nacos 地址
	NacosAddr    string `json:"nacosAddr"`
	NacosDataID  string `json:"nacosDataId"`
	NacosGroup   string `json:"nacosGroup"`
	NacosNamespace string `json:"nacosNamespace"` // 空 = public 命名空间

	// 本地配置文件路径(如 /opt/app/application.yml)
	LocalPath string `json:"localPath"`

	// jar 包路径(如 /opt/app/demo.jar)
	JarPath string `json:"jarPath"`
	// jar 内配置文件的相对路径(如 BOOT-INF/classes/application.yml)
	JarEntry string `json:"jarEntry"`
}

// ConfigSnapshot 是采集结果: 三类配置各自的内容 + 采集错误。
// 采集器对每个源独立采集, 一个失败不影响其他, 错误记录在对应字段。
type ConfigSnapshot struct {
	Nacos  string `json:"nacos"`   // Nacos 配置内容, 失败则为空
	Local  string `json:"local"`   // 本地文件内容
	Jar    string `json:"jar"`     // jar 内配置内容
	NacosErr  string `json:"nacosErr"`
	LocalErr  string `json:"localErr"`
	JarErr    string `json:"jarErr"`
}

// CollectConfigs 根据配置源描述, 采集三类配置, 返回快照。
// 三个采集互相独立, 单个失败不影响其他(错误记录在 snapshot 的 Err 字段)。
func CollectConfigs(src ConfigSources) ConfigSnapshot {
	var snap ConfigSnapshot

	// 1. Nacos: 走 OpenAPI GET /nacos/v1/cs/configs
	if src.NacosAddr != "" && src.NacosDataID != "" {
		snap.Nacos, snap.NacosErr = fetchNacosConfig(src)
	}

	// 2. 本地配置文件
	if src.LocalPath != "" {
		b, err := os.ReadFile(src.LocalPath)
		if err != nil {
			snap.LocalErr = err.Error()
		} else {
			snap.Local = string(b)
		}
	}

	// 3. jar 内配置
	if src.JarPath != "" && src.JarEntry != "" {
		snap.Jar, snap.JarErr = readJarEntry(src.JarPath, src.JarEntry)
	}

	return snap
}

// fetchNacosConfig 调 Nacos OpenAPI 拉配置。
// Nacos 的配置查询接口: GET /nacos/v1/cs/configs?dataId=xx&group=xx
func fetchNacosConfig(src ConfigSources) (string, string) {
	url := strings.TrimRight(src.NacosAddr, "/") + "/nacos/v1/cs/configs"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err.Error()
	}
	q := req.URL.Query()
	q.Set("dataId", src.NacosDataID)
	q.Set("group", src.NacosGroup)
	if src.NacosNamespace != "" {
		q.Set("tenant", src.NacosNamespace)
	}
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "连接 Nacos 失败: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Sprintf("Nacos 返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err.Error()
	}
	return string(body), ""
}

// readJarEntry 从 jar(zip)包内读取指定 entry 的内容。
// Spring Boot 的 jar 里配置通常在 BOOT-INF/classes/application.yml。
func readJarEntry(jarPath, entryName string) (string, string) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return "", "打开 jar 失败: " + err.Error()
	}
	defer r.Close()

	for _, f := range r.File {
		// entry 路径可能带或不带前导斜杠, 统一比较
		if strings.TrimPrefix(f.Name, "/") == strings.TrimPrefix(entryName, "/") {
			rc, err := f.Open()
			if err != nil {
				return "", err.Error()
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				return "", err.Error()
			}
			return string(b), ""
		}
	}
	return "", fmt.Sprintf("jar 内未找到 %s", entryName)
}

// snapshotToJSON 把采集结果编码成 JSON, 用于塞进 CommandResult.Output 回传。
func snapshotToJSON(s ConfigSnapshot) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// snapshotFromJSON 从 CommandResult.Output 解析回采集结果(控制面端用)。
func snapshotFromJSON(s string) (ConfigSnapshot, error) {
	var snap ConfigSnapshot
	err := json.Unmarshal([]byte(s), &snap)
	return snap, err
}

// 占位: 避免未导入 filepath 的编译错误(jar 读取后续可能用到)
