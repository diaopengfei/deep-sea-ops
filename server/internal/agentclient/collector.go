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
	NacosAddr      string `json:"nacosAddr"`
	NacosDataID    string `json:"nacosDataId"`
	NacosGroup     string `json:"nacosGroup"`
	NacosNamespace string `json:"nacosNamespace"` // 空 = public 命名空间

	// Nacos 认证(生产环境通常开启鉴权):
	// 优先用 accessToken; 没有则用 username/password 登录获取。
	NacosAccessToken string `json:"nacosAccessToken"`
	NacosUsername    string `json:"nacosUsername"`
	NacosPassword    string `json:"nacosPassword"`

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
	Nacos    string `json:"nacos"`
	Local    string `json:"local"`
	Jar      string `json:"jar"`
	NacosErr string `json:"nacosErr"`
	LocalErr string `json:"localErr"`
	JarErr   string `json:"jarErr"`
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
// 认证策略: 有 accessToken 直接带; 否则用 username/password 登录获取 token; 都没有则裸调(适用于未开鉴权的 Nacos)。
func fetchNacosConfig(src ConfigSources) (string, string) {
	baseURL := strings.TrimRight(src.NacosAddr, "/")
	httpClient := &http.Client{Timeout: 5 * time.Second}

	// 确定 accessToken: 优先用传入的, 没有则登录获取
	token := src.NacosAccessToken
	if token == "" && src.NacosUsername != "" && src.NacosPassword != "" {
		var err string
		token, err = nacosLogin(httpClient, baseURL, src.NacosUsername, src.NacosPassword)
		if err != "" {
			return "", "Nacos 登录失败: " + err
		}
	}

	// 构造配置查询请求: GET /nacos/v1/cs/configs?dataId=xx&group=xx[&tenant=xx][&accessToken=xx]
	url := baseURL + "/nacos/v1/cs/configs"
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
	if token != "" {
		q.Set("accessToken", token)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := httpClient.Do(req)
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

// nacosLogin 用 username/password 调 Nacos 登录接口获取 accessToken。
// Nacos 登录接口: POST /nacos/v1/auth/login (表单: username, password)
func nacosLogin(client *http.Client, baseURL, username, password string) (string, string) {
	url := baseURL + "/nacos/v1/auth/login"
	req, err := http.NewRequest("POST", url, strings.NewReader("username="+username+"&password="+password))
	if err != nil {
		return "", err.Error()
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Sprintf("登录返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err.Error()
	}

	// Nacos 登录响应: {"accessToken":"xxx","tokenTtl":...", ...}
	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "解析登录响应失败: " + err.Error()
	}
	if result.AccessToken == "" {
		return "", "登录响应中无 accessToken"
	}
	return result.AccessToken, ""
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