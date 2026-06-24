package agentclient

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// executeDeploy 在本节点部署一个 Java 项目。
//
// 流程:
//  1. 创建项目目录(通过平台抽象层, 自动适配 Linux/Windows 路径)
//  2. 如果 jarPath 是本机路径, 复制到项目目录; 如果不存在, 跳过(假设已在目标节点)
//  3. 写入配置文件 application.yml(如果 configText 非空)
//  4. 通过平台抽象层启动 Java 进程(Linux: java, Windows: javaw)
//  5. 返回启动日志
func executeDeploy(params map[string]string) (string, error) {
	jarPath := params["jarPath"]
	configText := params["configText"]
	projectName := params["projectName"]
	if projectName == "" {
		projectName = "app"
	}

	// 1. 创建项目目录(通过平台抽象层, 自动适配 /opt vs ProgramData)
	deployDir := resolveDeployDir(projectName)
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

	// 打开日志文件
	logFile := filepath.Join(deployDir, "app.log")
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer logF.Close()

	// 通过平台抽象层启动 Java 进程
	pid, err := startJavaProcess(targetJar, deployDir, logF)
	if err != nil {
		return "", fmt.Errorf("启动项目失败: %w", err)
	}

	return fmt.Sprintf("项目 %s 已启动, PID=%d, 日志: %s, 部署目录: %s",
		projectName, pid, logFile, deployDir), nil
}

// executeStopProject 停止本节点上运行的项目。
// 按 projectPath 匹配进程, 找到后通过平台抽象层 kill。
func executeStopProject(params map[string]string) (string, error) {
	projectPath := params["projectPath"]
	if projectPath == "" {
		return "", fmt.Errorf("缺少参数 projectPath")
	}

	processes := ListProcesses()
	var stopped []string
	for _, p := range processes {
		if strings.Contains(p.CmdLine, projectPath) {
			// 通过平台抽象层 kill 进程(自动适配 kill/taskkill)
			if err := killProcessViaPlatform(p.PID); err != nil {
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

// resolveDeployDir 通过平台抽象层获取部署目录。
// 未初始化时回退到 /opt/<projectName>(保持向后兼容)。
func resolveDeployDir(projectName string) string {
	if globalOps != nil {
		return globalOps.Deploy.DeployDir(projectName)
	}
	return filepath.Join("/opt", projectName)
}

// startJavaProcess 通过平台抽象层启动 Java 进程。
// 由于需要设置工作目录和日志重定向, 这里用 exec.Command 直接启动,
// 但程序名(java/javaw)通过 Builder 决定。
func startJavaProcess(jarPath string, workDir string, logFile *os.File) (int, error) {
	if globalOps == nil {
		// 兜底: 未初始化时用 java -jar
		return startJavaWithRedirect("java", jarPath, workDir, logFile)
	}
	// 从 Builder 获取命令, 提取程序名(java/javaw)
	cmd := globalOps.Builder().StartJava(jarPath, nil)
	return startJavaWithRedirect(cmd.Name, jarPath, workDir, logFile)
}

// startJavaWithRedirect 用指定 java 程序名启动, 设置工作目录和日志重定向。
// 不通过 Executor.RunBackground, 因为需要设置 cmd.Dir 和 cmd.Stdout。
func startJavaWithRedirect(javaBin, jarPath string, workDir string, logFile *os.File) (int, error) {
	execCmd := exec.Command(javaBin, "-jar", jarPath)
	execCmd.Dir = workDir
	execCmd.Stdout = logFile
	execCmd.Stderr = logFile
	// 设置环境变量, 让 Spring Boot 读外部配置
	execCmd.Env = append(os.Environ(), "SPRING_CONFIG_LOCATION="+filepath.Join(workDir, "application.yml"))
	if err := execCmd.Start(); err != nil {
		return 0, err
	}
	return execCmd.Process.Pid, nil
}

// killProcessViaPlatform 通过平台抽象层 kill 进程。
// Linux: 先 SIGTERM 等 5 秒, 不退出则 SIGKILL; Windows: 直接 taskkill /F。
func killProcessViaPlatform(pid int) error {
	if globalOps == nil {
		return fmt.Errorf("平台抽象层未初始化, 无法停止进程 %d", pid)
	}
	return globalOps.Deploy.StopJava(pid)
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
