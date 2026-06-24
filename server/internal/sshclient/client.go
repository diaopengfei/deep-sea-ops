// Package sshclient 提供 SSH 远程操作能力, 用于自动注入。
//
// 功能: 用存储的 SSH 凭据(已解密)连接目标服务器, 上传二进制文件, 执行远程命令。
// 用于: SSH 推送 deepsea-agent/deepsea-server 二进制 + 配置, 远程拉起 systemd。
package sshclient

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/deepsea-ops/server/internal/shellutil"
)

// 默认命令执行超时
const defaultCmdTimeout = 60 * time.Second

// insecureWarned 控制未校验 HostKey 的警告只打印一次(避免日志刷屏)。
var insecureWarned sync.Once

// Client 封装一个 SSH 会话, 提供文件上传和命令执行能力。
type Client struct {
	client *ssh.Client
}

// Config 是连接一台远程服务器所需的参数(凭据已解密)。
type Config struct {
	Host     string // IP 地址
	Port     int    // SSH 端口, 默认 22
	Username string // SSH 用户名
	Password string // 明文密码(authType=password 时用)
	PrivateKey string // PEM 格式私钥(authType=key 时用)

	// HostKey 是可选的 SSH 主机公钥(PEM 格式)。
	// 设置后用 ssh.FixedHostKey 校验, 防止中间人攻击。
	// 留空则跳过校验(仅适用于内网可信环境), 启动时打印一次警告。
	HostKey string
}

// NewClient 用给定凭据建立 SSH 连接。
func NewClient(cfg Config) (*Client, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}

	authMethods := []ssh.AuthMethod{}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("未提供认证方式(密码或私钥)")
	}

	// HostKey 校验策略: 提供了 HostKey 则严格校验; 否则跳过(内网部署场景)并警告
	var hostKeyCb ssh.HostKeyCallback
	if cfg.HostKey != "" {
		pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(cfg.HostKey))
		if err != nil {
			return nil, fmt.Errorf("解析 HostKey 失败: %w", err)
		}
		hostKeyCb = ssh.FixedHostKey(pk)
	} else {
		insecureWarned.Do(func() {
			log.Println("[警告] SSH 未校验 HostKey (内网部署场景可接受, 生产环境建议配置 HostKey)")
		})
		hostKeyCb = ssh.InsecureIgnoreHostKey()
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCb,
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接 %s 失败: %w", addr, err)
	}
	return &Client{client: client}, nil
}

// Close 关闭 SSH 连接。
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// RunCommand 在远程服务器上执行一条命令, 返回合并的 stdout+stderr 输出。
// 命令执行有超时保护(defaultCmdTimeout), 超时后终止远程进程并关闭 session。
func (c *Client) RunCommand(cmd string) (string, error) {
	return c.RunCommandTimeout(cmd, defaultCmdTimeout)
}

// RunCommandTimeout 在远程服务器上执行一条命令, 可自定义超时。
// 超时后发送 SIGKILL 终止远程进程, 关闭 session, 返回超时错误。
func (c *Client) RunCommandTimeout(cmd string, timeout time.Duration) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建 session 失败: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	if err := session.Start(cmd); err != nil {
		return buf.String(), fmt.Errorf("命令启动失败: %w", err)
	}

	// 超时控制: 用 timer + goroutine 等待命令结束
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- session.Wait()
	}()

	select {
	case err := <-doneCh:
		if err != nil {
			return buf.String(), fmt.Errorf("命令执行失败: %w, 输出: %s", err, buf.String())
		}
		return buf.String(), nil
	case <-time.After(timeout):
		// 超时: 终止远程进程并关闭 session, 让等待中的 goroutine 收到错误退出
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		<-doneCh // 等待 goroutine 结束, 防止泄漏
		return buf.String(), fmt.Errorf("命令执行超时 (%v), 命令: %s, 输出: %s", timeout, cmd, buf.String())
	}
}

// UploadFile 通过 SCP 协议上传本地文件到远程服务器。
// remotePath 是远程目标路径(含文件名), 会自动创建父目录。
func (c *Client) UploadFile(localPath, remotePath string) error {
	if err := shellutil.SafePath(remotePath); err != nil {
		return fmt.Errorf("远程路径不安全: %w", err)
	}
	remoteDir := filepath.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		if _, err := c.RunCommand("mkdir -p " + shellutil.Quote(remoteDir)); err != nil {
			return fmt.Errorf("创建远程目录失败: %w", err)
		}
	}

	// 读取本地文件
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("读取本地文件失败: %w", err)
	}

	// 用 SCP 协议写入
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("创建 session 失败: %w", err)
	}
	defer session.Close()

	w, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("获取 stdin 失败: %w", err)
	}

	// SCP 协议: 先发文件头(C<mode> <size> <name>\n), 再发内容, 最后发 \0
	go func() {
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", 0o755, len(data), filepath.Base(remotePath))
		w.Write(data)
		w.Write([]byte{0})
	}()

	// 远程用 scp -t 接收, 路径用 shellutil.Quote 转义
	if err := session.Run("scp -t " + shellutil.Quote(remoteDir)); err != nil {
		return fmt.Errorf("SCP 上传失败: %w", err)
	}
	return nil
}

// UploadContent 把内存内容作为文件上传到远程服务器。
func (c *Client) UploadContent(content []byte, remotePath string) error {
	if err := shellutil.SafePath(remotePath); err != nil {
		return fmt.Errorf("远程路径不安全: %w", err)
	}
	remoteDir := filepath.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		if _, err := c.RunCommand("mkdir -p " + shellutil.Quote(remoteDir)); err != nil {
			return fmt.Errorf("创建远程目录失败: %w", err)
		}
	}

	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("创建 session 失败: %w", err)
	}
	defer session.Close()

	w, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("获取 stdin 失败: %w", err)
	}

	go func() {
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", 0o644, len(content), filepath.Base(remotePath))
		w.Write(content)
		w.Write([]byte{0})
	}()

	if err := session.Run("scp -t " + shellutil.Quote(remoteDir)); err != nil {
		return fmt.Errorf("SCP 上传失败: %w", err)
	}
	return nil
}
