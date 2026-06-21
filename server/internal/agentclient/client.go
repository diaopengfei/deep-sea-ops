package agentclient

import (
	"context"
	"log"
	"net"
	"os"
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
}

// New 用本机信息创建 Agent 客户端。
func New(agentID, serverAddr string) (*Client, error) {
	hostname, _ := os.Hostname()
	// 取本机第一个非回环 IP 作为上报地址
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

// Run 建立 gRPC 流, 发注册, 启动心跳和接收两个循环, 阻塞直到断开。
func (c *Client) Run(ctx context.Context) error {
	client := pb.NewAgentServiceClient(c.conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	c.stream = stream

	// 第一步: 发注册
	if err := stream.Send(&pb.AgentMessage{
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
	go c.recvLoop()

	// 心跳循环: 每 5 秒发一次
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				return err
			}
		}
	}
}

func (c *Client) sendHeartbeat() error {
	return c.stream.Send(&pb.AgentMessage{
		Payload: &pb.AgentMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				AgentId:    c.agentID,
				Timestamp:  time.Now().UnixMilli(),
				CpuPercent: 0, // M4 之后再接真实指标
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
			log.Printf("收到指令: id=%s type=%s params=%v", p.Command.CommandId, p.Command.Type, p.Command.Params)
			// M4 在这里执行真实指令并回传 CommandResult
		}
	}
}

// Close 关闭连接。
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