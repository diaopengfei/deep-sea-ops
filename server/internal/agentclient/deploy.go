package agentclient

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// executeDeploy 在本节点部署一个 Java 项目。
//
// 流程:
//  1. 创建项目目录 /opt/<projectName>(不存在则创建)
//  2. 如果 jarPath 是本机路径, 复制到项目目录; 如果不存在, 跳过(假设已在目标节点)
//  3. 写入配置文件 application.yml(如果 configText 非空)
//  4. 用 nohup java -jar 启动项目(Linux) 或 javaw(Windows)
//  5. 返回启动日志
//
// 注意: 当前实现是本地部署(jar 已在节点上或通过其他方式分发)。
// v0.4 会增加 SSH 远程推送二进制能力, 这里先实现本地拉起逻辑。
func executeDeploy(params map[string]string) (string, error) {
	jarPath := params["jarPath"]
	configText := params["configText"]
	projectName := params["projectName"]
	if projectName == "" {
		projectName = "app"
	}

	// 1. 创建项目目录
	deployDir := filepath.Join("/opt", projectName)
	if runtime.GOOS == "windows" {
		deployDir = filepath.Join(os.Getenv("ProgramData"), "deepsea", projectName)
	}
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		return "", fmt.Errorf("创建部署目录失败: %w", err)
	}

	// 2. 处理 jar 包: 如果 jarPath 存在且是本机文件, 复制到部署目录
	targetJar := filepath.Join(deployDir, filepath.Base(jarPath))
	if jarPath != "" {
		if _, err := os.Stat(jarPath); err == nil {
			// 源 jar 存在, 复制到部署目录(如果不同)
			if jarPath != targetJar {
				if err := copyFile(jarPath, targetJar); err != nil {
					return "", fmt.Errorf("复制 jar 失败: %w", err)
				}
			}
		} else {
			// 源 jar 不存在, 检查目标 jar 是否已存在
			if _, err := os.Stat(targetJar); err != nil {
				return "", fmt.Errorf("jar 包不存在: %s", jarPath)
			}
		}
	}

	// 3. 写入配置文件
	if configText != "" {
		configPath := filepath.Join(deployDir, "application.yml")
		if err := os.WriteFile(configPath, []byte(configText), 0o644); err != nil {
			return "", fmt.Errorf("写入配置文件失败: %w", err)
		}
	}

	// 4. 启动项目
	// 检查是否已有同名进程在运行, 避免重复启动
	processes := ListProcesses()
	for _, p := range processes {
		if strings.Contains(p.CmdLine, targetJar) {
			return fmt.Sprintf("项目已在运行(PID=%d), 跳过启动", p.PID), nil
		}
	}

	// 用 nohup 后台启动(Linux) 或 start /B(Windows)
	var cmd *exec.Cmd
	logFile := filepath.Join(deployDir, "app.log")
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer log.Close()

	if runtime.GOOS == "windows" {
		cmd = exec.Command("javaw", "-jar", targetJar)
	} else {
		cmd = exec.Command("java", "-jar", targetJar)
	}
	cmd.Dir = deployDir
	cmd.Stdout = log
	cmd.Stderr = log
	// 设置环境变量, 让 Spring Boot 读外部配置
	cmd.Env = append(os.Environ(), "SPRING_CONFIG_LOCATION="+filepath.Join(deployDir, "application.yml"))

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动项目失败: %w", err)
	}

	return fmt.Sprintf("项目 %s 已启动, PID=%d, 日志: %s, 部署目录: %s",
		projectName, cmd.Process.Pid, logFile, deployDir), nil
}

// executeStopProject 停止本节点上运行的项目。
// 按 projectPath 匹配进程, 找到后 kill。
func executeStopProject(params map[string]string) (string, error) {
	projectPath := params["projectPath"]
	if projectPath == "" {
		return "", fmt.Errorf("缺少参数 projectPath")
	}

	processes := ListProcesses()
	var stopped []string
	for _, p := range processes {
		if strings.Contains(p.CmdLine, projectPath) {
			// kill 进程
			if err := killProcess(p.PID); err != nil {
				return "", fmt.Errorf("停止进程 %d 失败: %w", p.PID, err)
			}
			stopped = append(stopped, fmt.Sprintf("PID=%d(%s)", p.PID, p.Name))
		}
	}

	if len(stopped) == 0 {
		return "未找到匹配的运行进程", nil
	}
	return fmt.Sprintf("已停止 %d 个进程: %s", len(stopped), strings.Join(stopped, ", ")), nil
}

// killProcess 跨平台停止进程, 发送 SIGTERM 后等待最多 5 秒退出。
func killProcess(pid int) error {
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid)).Run()
	}
	// 先发 SIGTERM, 优雅退出
	if err := exec.Command("kill", "-15", fmt.Sprintf("%d", pid)).Run(); err != nil {
		return err
	}
	// 等待最多 5 秒, 检查进程是否还在
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	// 仍在运行, 强制 SIGKILL
	return exec.Command("kill", "-9", fmt.Sprintf("%d", pid)).Run()
}

// processExists 检查指定 PID 的进程是否还存在(Linux/macOS)。
func processExists(pid int) bool {
	if runtime.GOOS == "windows" {
		return true // Windows 用 taskkill /F, 不需要等待
	}
	// kill -0 只检查进程是否存在, 不发信号
	return exec.Command("kill", "-0", fmt.Sprintf("%d", pid)).Run() == nil
}

// copyFile 复制文件, 用 io.Copy 流式复制避免大 jar 文件 OOM。
func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstF.Close()
	_, err = io.Copy(dstF, srcF)
	return err
}
