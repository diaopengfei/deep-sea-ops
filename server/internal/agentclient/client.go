package agentclient

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/deepsea-ops/server/internal/proto/agent"
)

// Client 是 Agent 端: 连接控制面, 发心跳, 收指令。
type Client struct {
	agentID  string
	hostname string
	ip       string
	conn     *grpc.ClientConn
	stream   pb.AgentService_ConnectClient
	streamMu sync.Mutex // 保护 stream.Send, 心跳和指令结果可能并发发送
	wg       sync.WaitGroup
}

// New 用本机信息创建 Agent 客户端。
func New(agentID, serverAddr string) (*Client, error) {
	hostname, _ := os.Hostname()
	ip := localIP()

	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{
		agentID:  agentID,
		hostname: hostname,
		ip:      ip,
		conn:    conn,
	}, nil
}

// send 加锁发送, gRPC stream.Send 不是并发安全的。
func (c *Client) send(msg *pb.AgentMessage) error {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	return c.stream.Send(msg)
}

// Run 建立 gRPC 流, 发注册, 启动心跳和接收两个循环, 阻塞直到断开或 ctx 取消。
func (c *Client) Run(ctx context.Context) error {
	client := pb.NewAgentServiceClient(c.conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	c.stream = stream

	// 第一步: 发注册
	if err := c.send(&pb.AgentMessage{
		Payload: &pb.AgentMessage_Register{
			Register: &pb.RegisterRequest{
				AgentId: c.agentID, Hostname: c.hostname, Ip: c.ip,
			},
		},
	}); err != nil {
		return err
	}
	log.Printf("Agent %s 已注册 (host=%s ip=%s)", c.agentID, c.hostname, c.ip)

	// 接收循环: 收控制面下发的消息(ACK/指令)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.recvLoop()
	}()

	// 心跳循环: 每 5 秒发一次
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = stream.CloseSend()
			c.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				c.wg.Wait()
				return err
			}
		}
	}
}

func (c *Client) sendHeartbeat() error {
	return c.send(&pb.AgentMessage{
		Payload: &pb.AgentMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				AgentId:    c.agentID,
				Timestamp:  time.Now().UnixMilli(),
				CpuPercent: 0,
				MemPercent: 0,
			},
		},
	})
}

// recvLoop 持续读取控制面下发的消息。
func (c *Client) recvLoop() {
	for {
		msg, err := c.stream.Recv()
		if err != nil {
			log.Printf("Agent 接收循环结束: %v", err)
			return
		}
		switch p := msg.Payload.(type) {
		case *pb.ServerMessage_RegisterAck:
			log.Printf("注册确认: accepted=%v msg=%s", p.RegisterAck.Accepted, p.RegisterAck.Message)
		case *pb.ServerMessage_Command:
			// 指令执行放独立 goroutine, 不阻塞接收后续消息(读大文件可能慢)
			c.wg.Add(1)
			go func(cmd *pb.Command) {
				defer c.wg.Done()
				c.executeCommand(cmd)
			}(p.Command)
		}
	}
}

// executeCommand 执行单条指令并回传结果。
// 目前支持 READ_CONFIG: 读 params["path"] 指定的文件内容。
func (c *Client) executeCommand(cmd *pb.Command) {
	result := &pb.CommandResult{CommandId: cmd.CommandId}

	switch cmd.Type {
	case "READ_CONFIG":
		path := cmd.Params["path"]
		if path == "" {
			result.Success = false
			result.Error = "缺少参数 path"
		} else {
			content, err := os.ReadFile(path)
			if err != nil {
				result.Success = false
				result.Error = err.Error()
			} else {
				result.Success = true
				result.Output = string(content)
			}
		}
	case "COLLECT_CONFIGS":
		// 采集三类配置源: Nacos / 本地配置文件 / jar 内配置
		// 从 params 构造 ConfigSources, 调采集器, 结果 JSON 编码后回传
		src := ConfigSources{
			NacosAddr:      cmd.Params["nacosAddr"],
			NacosDataID:    cmd.Params["nacosDataId"],
			NacosGroup:     cmd.Params["nacosGroup"],
			NacosNamespace: cmd.Params["nacosNamespace"],
			LocalPath:      cmd.Params["localPath"],
			JarPath:        cmd.Params["jarPath"],
			JarEntry:       cmd.Params["jarEntry"],
		}
		snap := CollectConfigs(src)
		result.Success = true
		result.Output = snapshotToJSON(snap)
	case "SCAN_PROJECTS":
		// 扫描节点上的 Java/Python 项目
		// 扫描节点上的 Java/Python 项目
		dirsParam := cmd.Params["scanDirs"]
		if dirsParam == "" { dirsParam = "/home,/data" }
		scanDirs := strings.Split(dirsParam, ",")
		scanResult := ScanProjects(scanDirs, 5)
		data, jerr := json.Marshal(scanResult)
		if jerr != nil {
			result.Success = false
			result.Error = "序列化扫描结果失败: " + jerr.Error()
		} else {
			result.Success = true
			result.Output = string(data)
		}
	default:
		result.Success = false
		result.Error = "未知指令类型: " + cmd.Type
	}

	if err := c.send(&pb.AgentMessage{
		Payload: &pb.AgentMessage_Result{Result: result},
	}); err != nil {
		log.Printf("回传指令结果失败: %v", err)
	}
}

// Close 关闭 gRPC 连接。
func (c *Client) Close() error {
	return c.conn.Close()
}

// localIP 返回本机首选非回环 IPv4。
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}
