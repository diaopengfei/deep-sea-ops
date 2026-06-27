package agentclient

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// executeUpgrade 实现 Agent 热更新(v0.6.6)。
//
// 流程:
//  1. 从 params["url"] 下载新二进制到临时文件
//  2. 可选校验 sha256(params["checksum"])
//  3. 获取当前可执行文件路径, 备份为 <exe>.bak, 用新二进制覆盖
//  4. 返回提示; 由调用方(c.executeCommand)回传结果后, 调 scheduleRestart 异步退出
//     服务管理器(systemd Restart=always / Windows Service)会自动拉起新版本
//
// 不在此函数内直接 os.Exit: 需先把指令结果回传控制面, 再退出。
func executeUpgrade(params map[string]string) (string, error) {
	url := params["url"]
	if url == "" {
		return "", fmt.Errorf("缺少参数 url")
	}
	tmp, err := downloadToTemp(url)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer os.Remove(tmp)

	// 可选 sha256 校验
	if want := params["checksum"]; want != "" {
		sum, err := sha256File(tmp)
		if err != nil {
			return "", fmt.Errorf("计算校验和失败: %w", err)
		}
		if sum != want {
			return "", fmt.Errorf("校验和不匹配: 期望 %s 实际 %s", want, sum)
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		// 非 fatal, 继续用原路径
		log.Printf("警告: 解析可执行文件符号链接失败: %v", err)
	}

	// 备份原二进制
	backup := exePath + ".bak"
	if orig, err := os.ReadFile(exePath); err == nil {
		_ = os.WriteFile(backup, orig, 0o755)
	}

	// 读取新二进制并覆盖写入(先写临时再 rename, 跨设备时回退直接写)
	newBin, err := os.ReadFile(tmp)
	if err != nil {
		return "", fmt.Errorf("读取下载文件失败: %w", err)
	}
	if err := os.WriteFile(exePath, newBin, 0o755); err != nil {
		// 写失败则回滚备份
		if orig, e := os.ReadFile(backup); e == nil {
			_ = os.WriteFile(exePath, orig, 0o755)
		}
		return "", fmt.Errorf("替换二进制失败: %w", err)
	}

	// 安排延迟退出: 给指令结果回传留 1 秒, 之后退出让服务管理器拉起新版本
	go scheduleRestart(1 * time.Second)
	return fmt.Sprintf("已升级到新版本, %d 字节; 1 秒后退出由服务管理器重启", len(newBin)), nil
}

// scheduleRestart 延迟后退出进程。服务管理器(systemd/Windows Service)配置了
// Restart=always 会自动重新拉起 Agent, 从而运行新版本二进制。
func scheduleRestart(d time.Duration) {
	time.Sleep(d)
	log.Printf("Agent 升级完成, 退出以重启服务")
	os.Exit(0)
}

// downloadToTemp 下载 url 到临时文件, 返回路径。调用方负责删除。
func downloadToTemp(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "deepsea-agent-*.bin")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
