package grpcserver

import (
	"fmt"
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
	done     chan struct{}          // 关闭时通知 send goroutine 退出
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
//
// 协议约定: Agent 连上后的第一条消息必须是 Register。
// 收到 Register 后才启动下行发送 goroutine, 这样 conn 在 goroutine 启动时已确定,
// 避免了"主循环写 conn + send goroutine 读 conn"的数据竞争。
func (s *Server) Connect(stream pb.AgentService_ConnectServer) error {
	// 1. 等待 Register 作为第一条消息
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

	// 2. 注册成功后启动下行发送 goroutine, conn 在此处捕获, 不再有竞争
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

	// 3. 主循环: 读 Agent 上行消息
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
			log.Printf("收到 Agent %s 指令结果: %s success=%v", conn.id, p.Result.CommandId, p.Result.Success)
		}
	}

	// 4. 连接断开: 通知 send goroutine 退出, 并从注册表移除(仅当仍是当前 conn)
	s.removeAgent(conn)
	<-sendDone
	return nil
}

// handleRegister 处理注册消息: 记录 Agent 信息, 回复 ACK。
// 若同一 ID 的旧连接仍在, 关闭其 done 通道使其 send goroutine 退出,
// 避免旧连接清理时误删新连接(重连竞态)。
func (s *Server) handleRegister(req *pb.RegisterRequest, stream pb.AgentService_ConnectServer) *agentConn {
	c := &agentConn{
		id:       req.AgentId,
		hostname: req.Hostname,
		ip:       req.Ip,
		lastSeen: time.Now(),
		send:     make(chan *pb.ServerMessage, 16),
		done:     make(chan struct{}),
	}

	s.mu.Lock()
	if old, ok := s.agents[req.AgentId]; ok {
		// 旧连接还挂着: 通知它退出, 它的 removeAgent 会发现已被替换而不删新 conn
		close(old.done)
	}
	s.agents[req.AgentId] = c
	s.mu.Unlock()

	// 回复 ACK
	_ = stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_RegisterAck{
			RegisterAck: &pb.RegisterAck{Accepted: true, Message: "注册成功"},
		},
	})
	return c
}

// removeAgent 从注册表移除 conn, 但仅当 map 里存的仍是同一个 conn。
// 这样旧连接断开时不会误删已被新连接替换的条目。
func (s *Server) removeAgent(c *agentConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.agents[c.id]; ok && cur == c {
		delete(s.agents, c.id)
		close(c.done)
		log.Printf("Agent %s 断开连接", c.id)
	}
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
	case <-c.done:
		return fmt.Errorf("agent %s 已断开", agentID)
	default:
		return fmt.Errorf("agent %s 发送通道已满", agentID)
	}
}