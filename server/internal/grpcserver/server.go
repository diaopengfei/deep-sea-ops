package grpcserver

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	pb "github.com/deepsea-ops/server/internal/proto/agent"
)

// Server 实现生成的 AgentServiceServer 接口, 处理 Agent 连接。
type Server struct {
	pb.UnimplementedAgentServiceServer

	mu       sync.RWMutex
	agents   map[string]*agentConn           // 在线 Agent 注册表
	results  map[string]chan *pb.CommandResult // commandID -> 结果等待通道
}

// agentConn 记录一个在线 Agent 的信息。
type agentConn struct {
	id       string
	hostname string
	ip       string
	lastSeen time.Time
	send     chan *pb.ServerMessage
	done     chan struct{}
}

func NewServer() *Server {
	return &Server{
		agents:  make(map[string]*agentConn),
		results: make(map[string]chan *pb.CommandResult),
	}
}

// AgentInfo 供 HTTP API 查询。
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

// Connect 双向流 RPC。Agent 第一条消息必须是 Register。
func (s *Server) Connect(stream pb.AgentService_ConnectServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	regMsg, ok := first.Payload.(*pb.AgentMessage_Register)
	if !ok {
		return fmt.Errorf("第一条消息必须是 Register, 实际收到 %T", first.Payload)
	}
	conn := s.handleRegister(regMsg.Register, stream)
	log.Printf("Agent 注册: id=%s host=%s ip=%s", regMsg.Register.AgentId, regMsg.Register.Hostname, regMsg.Register.Ip)

	ctx := stream.Context()
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-conn.done:
				return
			case msg := <-conn.send:
				if err := stream.Send(msg); err != nil {
					return
				}
			}
		}
	}()

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
		case *pb.AgentMessage_Heartbeat:
			s.handleHeartbeat(conn, p.Heartbeat)
		case *pb.AgentMessage_Result:
			s.handleResult(p.Result)
		}
	}

	s.removeAgent(conn)
	<-sendDone
	return nil
}

func (s *Server) handleRegister(req *pb.RegisterRequest, stream pb.AgentService_ConnectServer) *agentConn {
	c := &agentConn{
		id: req.AgentId, hostname: req.Hostname, ip: req.Ip,
		lastSeen: time.Now(),
		send:     make(chan *pb.ServerMessage, 16),
		done:     make(chan struct{}),
	}
	s.mu.Lock()
	if old, ok := s.agents[req.AgentId]; ok {
		close(old.done)
	}
	s.agents[req.AgentId] = c
	s.mu.Unlock()

	_ = stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_RegisterAck{
			RegisterAck: &pb.RegisterAck{Accepted: true, Message: "注册成功"},
		},
	})
	return c
}

func (s *Server) removeAgent(c *agentConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.agents[c.id]; ok && cur == c {
		delete(s.agents, c.id)
		close(c.done)
		log.Printf("Agent %s 断开连接", c.id)
	}
}

func (s *Server) handleHeartbeat(c *agentConn, hb *pb.Heartbeat) {
	s.mu.Lock()
	c.lastSeen = time.Now()
	s.mu.Unlock()
}

// handleResult 把 Agent 回传的结果投递到等待通道。
func (s *Server) handleResult(r *pb.CommandResult) {
	s.mu.Lock()
	ch, ok := s.results[r.CommandId]
	if ok {
		delete(s.results, r.CommandId)
	}
	s.mu.Unlock()
	if ok {
		ch <- r
		log.Printf("指令结果已投递: id=%s success=%v", r.CommandId, r.Success)
	} else {
		log.Printf("指令结果无等待者(可能已超时): id=%s", r.CommandId)
	}
}

// ReadConfig 向指定 Agent 下发读配置指令, 阻塞等待结果(带超时)。
// 供 HTTP API 调用。返回 (内容, 错误)。
func (s *Server) ReadConfig(agentID, path string, timeout time.Duration) (string, error) {
	s.mu.RLock()
	c, ok := s.agents[agentID]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("agent %s 不在线", agentID)
	}

	cmdID := uuid.NewString()
	ch := make(chan *pb.CommandResult, 1)
	s.mu.Lock()
	s.results[cmdID] = ch
	s.mu.Unlock()
	// 兜底清理: 超时后移除通道, 防止内存泄漏
	defer func() {
		s.mu.Lock()
		delete(s.results, cmdID)
		s.mu.Unlock()
	}()

	cmd := &pb.Command{
		CommandId: cmdID,
		Type:      "READ_CONFIG",
		Params:    map[string]string{"path": path},
	}
	select {
	case c.send <- &pb.ServerMessage{Payload: &pb.ServerMessage_Command{Command: cmd}}:
	case <-c.done:
		return "", fmt.Errorf("agent %s 已断开", agentID)
	case <-time.After(timeout):
		return "", fmt.Errorf("下发指令超时")
	}

	select {
	case r := <-ch:
		if !r.Success {
			return "", fmt.Errorf("agent 执行失败: %s", r.Error)
		}
		return r.Output, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("等待结果超时")
	}
}