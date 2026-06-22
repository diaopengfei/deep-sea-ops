package agentclient

import (
	"io/fs"
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
	Path         string      `json:"path"`         // 项目根目录或 jar 路径
	Type         ProjectType `json:"type"`          // 项目类型
	Name         string      `json:"name"`          // 项目名(目录名或 jar 文件名)
	ConfigFiles  []string    `json:"configFiles"`   // 配置文件路径列表(application.yml 等)
	JarPath      string      `json:"jarPath"`       // jar 路径(java-jar 类型)
	JarEntry     string      `json:"jarEntry"`      // jar 内默认配置 entry
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

// defaultScanDirs 返回默认扫描目录
func defaultScanDirs() []string {
	return []string{"/home", "/data"}
}

// parseScanDirs 从环境变量 SCAN_DIRS 解析扫描目录(逗号分隔)
func parseScanDirs() []string {
	dirs := os.Getenv("SCAN_DIRS")
	if dirs == "" {
		return defaultScanDirs()
	}
	var result []string
	for _, d := range strings.Split(dirs, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			result = append(result, d)
		}
	}
	if len(result) == 0 {
		return defaultScanDirs()
	}
	return result
}

// 确保使用 io/fs(避免 unused import 如果将来扩展)
var _ fs.DirEntry
