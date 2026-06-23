package agentclient

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectType 项目类型
type ProjectType string

const (
	ProjectJavaSpring ProjectType = "java-spring" // 含 application.yml/bootstrap.yml 的 Spring 项目
	ProjectJavaJar    ProjectType = "java-jar"     // 独立 jar 文件(Spring Boot fat jar)
	ProjectPython     ProjectType = "python"       // 含 requirements.txt 或 setup.py
)

// ProjectInfo 描述扫描到的一个项目
type ProjectInfo struct {
	Path         string         `json:"path"`         // 项目根目录或 jar 路径
	Type         ProjectType    `json:"type"`          // 项目类型
	Name         string         `json:"name"`          // 项目名(目录名或 jar 文件名)
	ConfigFiles  []string       `json:"configFiles"`   // 配置文件路径列表(application.yml 等)
	JarPath      string         `json:"jarPath"`       // jar 路径(java-jar 类型)
	JarEntry     string         `json:"jarEntry"`      // jar 内默认配置 entry

	// 以下字段在扫描后由 EnrichScanResult 填充:
	Running         bool            `json:"running"`         // 是否在运行(通过进程列表匹配)
	PID             int             `json:"pid"`             // 运行中的进程 PID, 未运行为 0
	EffectiveConfig *EffectiveConfig `json:"effectiveConfig"` // 三路合并后的生效配置(Spring 项目)
}

// ScanResult 是一次扫描的结果
type ScanResult struct {
	Projects  []ProjectInfo `json:"projects"`
	Hosts     string        `json:"hosts"`      // /etc/hosts 内容
	HostsErr  string        `json:"hostsErr"`   // 读 hosts 失败时的错误
	ScanErr   string        `json:"scanErr"`    // 扫描失败时的错误
}

// ScanProjects 扫描指定目录下的 Java/Python 项目, 并读取 hosts 文件。
// dirs: 扫描根目录列表(如 /home, /data)
// maxDepth: 递归最大深度(防无限递归), 0 表示不限制但默认上限 5
func ScanProjects(dirs []string, maxDepth int) ScanResult {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	result := ScanResult{Projects: []ProjectInfo{}}

	// 读 hosts 文件
	hosts, err := os.ReadFile("/etc/hosts")
	if err != nil {
		result.HostsErr = err.Error()
	} else {
		result.Hosts = string(hosts)
	}

	// 扫描每个目录
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil {
			continue // 目录不存在, 跳过
		}
		if !info.IsDir() {
			continue
		}
		scanDir(dir, maxDepth, 0, &result)
	}

	return result
}

// scanDir 递归扫描单个目录
func scanDir(dir string, maxDepth, depth int, result *ScanResult) {
	if depth > maxDepth {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			// 跳过隐藏目录和常见无关目录
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if isSkipDir(name) {
				continue
			}
			// 检查是否是 Spring 项目目录(含配置文件)
			if isSpringProjectDir(dir) {
				// 当前 dir 是 Spring 项目, 不再往下递归
				continue
			}
			// 递归扫描子目录
			scanDir(fullPath, maxDepth, depth+1, result)
		} else {
			// 文件: 检查是否是 jar
			if strings.HasSuffix(entry.Name(), ".jar") {
				result.Projects = append(result.Projects, ProjectInfo{
					Path:    fullPath,
					Type:    ProjectJavaJar,
					Name:    entry.Name(),
					JarPath: fullPath,
					JarEntry: "BOOT-INF/classes/application.yml",
				})
			}
		}
	}

	// 检查当前目录是否是 Spring 项目(含 application.yml 等)
	if isSpringProjectDir(dir) {
		configs := findConfigFiles(dir)
		if len(configs) > 0 {
			// 避免重复添加(子目录扫描时可能已加过)
			if !projectExists(result, dir, ProjectJavaSpring) {
				result.Projects = append(result.Projects, ProjectInfo{
					Path:        dir,
					Type:        ProjectJavaSpring,
					Name:        filepath.Base(dir),
					ConfigFiles: configs,
				})
			}
		}
	}

	// 检查是否是 Python 项目
	if isPythonProjectDir(dir) {
		if !projectExists(result, dir, ProjectPython) {
			result.Projects = append(result.Projects, ProjectInfo{
				Path: dir,
				Type: ProjectPython,
				Name: filepath.Base(dir),
			})
		}
	}
}

// isSpringProjectDir 检查目录是否含 Spring 配置文件
func isSpringProjectDir(dir string) bool {
	configNames := []string{
		"application.yml", "application.yaml", "application.properties",
		"bootstrap.yml", "bootstrap.yaml",
	}
	for _, name := range configNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
		// 也检查 resources 子目录(Spring 标准结构)
		resourcesPath := filepath.Join(dir, "src", "main", "resources", name)
		if _, err := os.Stat(resourcesPath); err == nil {
			return true
		}
	}
	return false
}

// findConfigFiles 找出目录中的所有配置文件
func findConfigFiles(dir string) []string {
	configNames := []string{
		"application.yml", "application.yaml", "application.properties",
		"bootstrap.yml", "bootstrap.yaml", "bootstrap.properties",
	}
	var found []string
	for _, name := range configNames {
		// 当前目录
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
		// resources 子目录
		rp := filepath.Join(dir, "src", "main", "resources", name)
		if _, err := os.Stat(rp); err == nil {
			found = append(found, rp)
		}
	}
	return found
}

// isPythonProjectDir 检查目录是否含 Python 项目标识文件
func isPythonProjectDir(dir string) bool {
	markers := []string{"requirements.txt", "setup.py", "pyproject.toml"}
	for _, name := range markers {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// isSkipDir 判断是否应跳过该目录(系统目录、缓存等)
func isSkipDir(name string) bool {
	skip := map[string]bool{
		"node_modules": true, "__pycache__": true, ".git": true,
		"target": true, "build": true, "dist": true,
		"bin": true, "obj": true, ".idea": true, ".vscode": true,
		"vendor": true, ".gradle": true, ".mvn": true,
	}
	return skip[name]
}

// projectExists 检查项目是否已存在于结果中(防重复)
func projectExists(result *ScanResult, path string, ptype ProjectType) bool {
	for _, p := range result.Projects {
		if p.Path == path && p.Type == ptype {
			return true
		}
	}
	return false
}

// EnrichScanResult 在扫描完成后, 为每个项目补充运行状态和生效配置。
//
// 做两件事:
//  1. 调 ListProcesses 获取当前进程列表, 匹配每个项目判断是否在运行
//  2. 对 Spring 项目: 读本地配置文件 → 提取 Nacos 地址 → 采集三路配置 → 合并出生效配置
//
// 对 Python 项目和独立 jar, 只做进程检测, 不做配置合并(Python 无统一配置格式, jar 的配置
// 已在 Spring 项目流程里覆盖)。
func EnrichScanResult(result *ScanResult) {
	if result == nil || len(result.Projects) == 0 {
		return
	}

	// 1. 获取进程列表(一次获取, 所有项目共用)
	processes := ListProcesses()

	for i := range result.Projects {
		p := &result.Projects[i]

		// 1a. 进程检测: 用项目路径匹配
		running, pid := IsProjectRunning(p.Path, processes)
		p.Running = running
		p.PID = pid

		// 1b. 对 Spring 项目做三路配置采集与合并
		if p.Type == ProjectJavaSpring && len(p.ConfigFiles) > 0 {
			p.EffectiveConfig = buildEffectiveConfig(p)
		}
	}
}

// buildEffectiveConfig 对单个 Spring 项目采集三路配置并合并。
//
// 流程:
//  1. 读本地配置文件(application.yml 等), 拼成一份本地配置文本
//  2. 从本地配置中提取 Nacos 地址(spring.cloud.nacos.config.server-addr 等)
//  3. 如果有 jar 包(同目录或子目录), 读 jar 内 BOOT-INF/classes/application.yml
//  4. 如果有 Nacos 地址, 调 Nacos OpenAPI 拉远程配置
//  5. 三路合并: jar(低) < 本地 < Nacos(高), 产出生效配置
func buildEffectiveConfig(p *ProjectInfo) *EffectiveConfig {
	// 1. 读本地配置文件, 拼接成一份文本
	var localText string
	var localErrs []string
	for _, cfgPath := range p.ConfigFiles {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			localErrs = append(localErrs, cfgPath+": "+err.Error())
			continue
		}
		if localText != "" {
			localText += "\n"
		}
		localText += string(b)
	}

	// 2. 从本地配置提取 Nacos 地址
	// 先展平成 KV, 再用 extractNacosAddr 查找
	localKV := flattenConfig(localText, "yml")
	nacosAddr := extractNacosAddr(localKV)

	// 同时提取 dataId / group(如果有自定义配置)
	nacosDataID := localKV["spring.cloud.nacos.config.data-id"]
	nacosGroup := localKV["spring.cloud.nacos.config.group"]
	if nacosGroup == "" {
		nacosGroup = "DEFAULT_GROUP"
	}
	// 从 spring.application.name 推断默认 dataId
	if nacosDataID == "" {
		if appName := localKV["spring.application.name"]; appName != "" {
			nacosDataID = appName + ".yml"
		}
	}

	// 3. 读 jar 内配置(如果有 jar)
	var jarText string
	var jarErr string
	jarPath := p.JarPath
	if jarPath == "" {
		// 尝试在项目目录下找 jar
		jarPath = findJarInDir(p.Path)
	}
	jarEntry := p.JarEntry
	if jarEntry == "" {
		jarEntry = "BOOT-INF/classes/application.yml"
	}
	if jarPath != "" {
		jarText, jarErr = readJarEntry(jarPath, jarEntry)
	} else {
		jarErr = "未找到 jar 包"
	}

	// 4. 采集 Nacos 远程配置
	var nacosText string
	var nacosErr string
	if nacosAddr != "" && nacosDataID != "" {
		src := ConfigSources{
			NacosAddr:      nacosAddr,
			NacosDataID:    nacosDataID,
			NacosGroup:     nacosGroup,
			NacosNamespace: localKV["spring.cloud.nacos.config.namespace"],
		}
		snap := CollectConfigs(src)
		nacosText = snap.Nacos
		nacosErr = snap.NacosErr
	} else {
		nacosErr = "未提取到 Nacos 地址或 dataId"
	}

	// 5. 三路合并
	ec := MergeConfigs(nacosText, localText, jarText)
	// 填充各源采集错误(供前端展示采集失败原因)
	ec.NacosErr = nacosErr
	ec.LocalErr = strings.Join(localErrs, "; ")
	ec.JarErr = jarErr
	return &ec
}

// findJarInDir 在项目目录下查找 jar 文件(取第一个)。
func findJarInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".jar") {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}
