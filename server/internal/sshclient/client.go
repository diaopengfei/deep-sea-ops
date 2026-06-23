// Package sshclient 提供 SSH 远程操作能力, 用于 v0.4 自动注入。
//
// 功能: 用存储的 SSH 凭据(已解密)连接目标服务器, 上传二进制文件, 执行远程命令。
// 用于: SSH 推送 deepsea-agent/deepsea-server 二进制 + 配置, 远程拉起 systemd。
package sshclient

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client 封装一个 SSH 会话, 提供文件上传和命令执行能力。
type Client struct {
	client *ssh.Client
}

// Config 是连接一台远程服务器所需的参数(凭据已解密)。
type Config struct {
	Host       string // IP 地址
	Port       int    // SSH 端口, 默认 22
	Username   string // SSH 用户名
	Password   string // 明文密码(authType=password 时用)
	PrivateKey string // PEM 格式私钥(authType=key 时用)
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

	sshCfg := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 简化: 不校验 host key(内网部署场景)
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
func (c *Client) RunCommand(cmd string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建 session 失败: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf
	if err := session.Run(cmd); err != nil {
		return buf.String(), fmt.Errorf("命令执行失败: %w, 输出: %s", err, buf.String())
	}
	return buf.String(), nil
}

// UploadFile 通过 SCP 协议上传本地文件到远程服务器。
// remotePath 是远程目标路径(含文件名), 会自动创建父目录。
func (c *Client) UploadFile(localPath, remotePath string) error {
	// 确保远程目录存在
	remoteDir := filepath.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		if _, err := c.RunCommand("mkdir -p " + remoteDir); err != nil {
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

	// 远程用 scp -t 接收
	if err := session.Run("scp -t " + remoteDir); err != nil {
		return fmt.Errorf("SCP 上传失败: %w", err)
	}
	return nil
}

// UploadContent 把内存内容作为文件上传到远程服务器。
func (c *Client) UploadContent(content []byte, remotePath string) error {
	remoteDir := filepath.Dir(remotePath)
	if remoteDir != "." && remoteDir != "/" {
		if _, err := c.RunCommand("mkdir -p " + remoteDir); err != nil {
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

	if err := session.Run("scp -t " + remoteDir); err != nil {
		return fmt.Errorf("SCP 上传失败: %w", err)
	}
	return nil
}

// HostPort 返回 host:port 字符串(用于日志)。
func (c *Config) HostPort() string {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(c.Host, fmt.Sprintf("%d", port))
}

// 保留 io 引用(UploadFile 内部用到了 io.Writer 接口)
var _ = io.Discard
