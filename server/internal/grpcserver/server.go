package grpcserver

import (
	"fmt"
	"context"
	"io"
	"log"
	"sync"
	"time"

	pb "github.com/deepsea-ops/server/internal/proto/agent"
)

// Server 实现生成的 AgentServiceServer 接口, 处理 Agent 连接。
type Server struct {
	pb.UnimplementedAgentServiceServer // 嵌入未实现基类, 避免 proto 升级后编译失败

	mu     sync.RWMutex
	agents map[string]*agentConn // 在线 Agent 注册表, key=agent_id
}

// agentConn 记录一个在线 Agent 的信息。
type agentConn struct {
	id       string
	hostname string
	ip       string
	lastSeen time.Time
	send     chan *pb.ServerMessage // 下行通道: 往这个 Agent 推消息
}

func NewServer() *Server {
	return &Server{agents: make(map[string]*agentConn)}
}

// ListAgents 返回当前在线 Agent 列表(供 HTTP API 查询)。
type AgentInfo struct {
	ID       string    `json:"id"`
	Hostname string    `json:"hostname"`
	IP       string    `json:"ip"`
	LastSeen time.Time `json:"lastSeen"`
}

func (s *Server) ListAgents() []AgentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentInfo, 0, len(s.agents))
	for _, c := range s.agents {
		out = append(out, AgentInfo{
			ID: c.id, Hostname: c.hostname, IP: c.ip, LastSeen: c.lastSeen,
		})
	}
	return out
}

// Connect 是双向流 RPC 的服务端实现。
// 流程: Agent 建连 -> 第一条消息应是 Register -> 循环读 Agent 上行消息
//
//	-> 同时有个 goroutine 从 send 通道读下行消息发给 Agent。
//
// 连接断开时从注册表移除。
func (s *Server) Connect(stream pb.AgentService_ConnectServer) error {
	var conn *agentConn

	// 后台 goroutine: 把 send 通道里的下行消息发给 Agent
	ctx := stream.Context()
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-s.recvChanFor(conn):
				if !ok {
					return
				}
				if err := stream.Send(msg); err != nil {
					return
				}
			}
		}
	}()

	// 主循环: 读 Agent 上行消息
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("agent 流接收错误: %v", err)
			break
		}

		switch p := msg.Payload.(type) {
		case *pb.AgentMessage_Register:
			conn = s.handleRegister(p.Register, stream)
		case *pb.AgentMessage_Heartbeat:
			if conn != nil {
				s.handleHeartbeat(conn, p.Heartbeat)
			}
		case *pb.AgentMessage_Result:
			if conn != nil {
				log.Printf("收到 Agent %s 指令结果: %s success=%v", conn.id, p.Result.CommandId, p.Result.Success)
			}
		}
	}

	// 连接断开: 清理注册表
	if conn != nil {
		s.mu.Lock()
		delete(s.agents, conn.id)
		s.mu.Unlock()
		log.Printf("Agent %s 断开连接", conn.id)
	}
	<-sendDone
	return nil
}

// recvChanFor 返回 conn 的下行通道(未注册时返回 nil, goroutine 会因 nil 通道阻塞)。
func (s *Server) recvChanFor(conn *agentConn) <-chan *pb.ServerMessage {
	if conn == nil {
		return nil
	}
	return conn.send
}

// handleRegister 处理注册消息: 记录 Agent 信息, 回复 ACK。
func (s *Server) handleRegister(req *pb.RegisterRequest, stream pb.AgentService_ConnectServer) *agentConn {
	c := &agentConn{
		id:       req.AgentId,
		hostname: req.Hostname,
		ip:       req.Ip,
		lastSeen: time.Now(),
		send:     make(chan *pb.ServerMessage, 16),
	}
	s.mu.Lock()
	s.agents[req.AgentId] = c
	s.mu.Unlock()
	log.Printf("Agent 注册: id=%s host=%s ip=%s", req.AgentId, req.Hostname, req.Ip)

	// 回复 ACK
	_ = stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_RegisterAck{
			RegisterAck: &pb.RegisterAck{Accepted: true, Message: "注册成功"},
		},
	})
	return c
}

// handleHeartbeat 更新 lastSeen(实际 CPU/内存后续处理)。
func (s *Server) handleHeartbeat(c *agentConn, hb *pb.Heartbeat) {
	s.mu.Lock()
	c.lastSeen = time.Now()
	s.mu.Unlock()
}

// SendCommand 向指定 Agent 下发指令(供其他模块调用, 如 HTTP 触发)。
func (s *Server) SendCommand(agentID string, cmd *pb.Command) error {
	s.mu.RLock()
	c, ok := s.agents[agentID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent %s 不在线", agentID)
	}
	select {
	case c.send <- &pb.ServerMessage{Payload: &pb.ServerMessage_Command{Command: cmd}}:
		return nil
	default:
		return fmt.Errorf("agent %s 发送通道已满", agentID)
	}
}

// 用 context 防止 unused 警告(后续接口会用到)
var _ = context.Background