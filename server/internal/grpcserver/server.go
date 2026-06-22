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

// Server 实现 gRPC 的 AgentServiceServer 接口, 是控制面管理所有 Agent 连接的核心。
//
// 职责:
//   1. 维护在线 Agent 注册表(谁连着、最后心跳时间)
//   2. 向指定 Agent 下发指令(如读配置), 并收集回传结果
//   3. 给 HTTP API 层提供查询接口(ListAgents / ReadConfig)
//
// 并发模型: Agent 连接是双向流, 每条连接有两个 goroutine(收/发)。
// agents 和 results 两个 map 用 sync.RWMutex 保护, 允许多读单写。
type Server struct {
	pb.UnimplementedAgentServiceServer // 嵌入基类, proto 升级新增方法时不会编译失败

	mu      sync.RWMutex                         // 保护下面两个 map
	agents  map[string]*agentConn                // 在线 Agent 注册表, key=agent_id
	results map[string]chan *pb.CommandResult    // 指令结果等待表, key=command_id
}

// agentConn 记录一个在线 Agent 的运行时状态。
// 每条 Agent 连接对应一个 agentConn, 连接断开时从注册表移除。
type agentConn struct {
	id       string              // Agent 唯一 ID(注册时上报)
	hostname string              // Agent 所在主机名
	ip       string              // Agent 上报的本机 IP
	lastSeen time.Time           // 最后一次心跳时间, 用于判断是否存活
	send     chan *pb.ServerMessage // 下行通道: 控制面向该 Agent 推消息(指令等)
	done     chan struct{}        // 关闭信号: 连接断开或被新连接替换时关闭, 通知 send goroutine 退出
}

// NewServer 创建一个空的 Agent 管理器。
func NewServer() *Server {
	return &Server{
		agents:  make(map[string]*agentConn),
		results: make(map[string]chan *pb.CommandResult),
	}
}

// AgentInfo 是给 HTTP API 返回的 Agent 信息(精简版, 不含内部通道)。
type AgentInfo struct {
	ID       string    `json:"id"`
	Hostname string    `json:"hostname"`
	IP       string    `json:"ip"`
	LastSeen time.Time `json:"lastSeen"`
}

// ListAgents 返回当前所有在线 Agent 的信息, 供 /api/agents 接口调用。
// 读操作用 RLock, 允许和心跳更新并发。
func (s *Server) ListAgents() []AgentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AgentInfo, 0, len(s.agents))  // 预分配, 空 bucket 也返回 [] 而非 null
	for _, c := range s.agents {
		out = append(out, AgentInfo{
			ID: c.id, Hostname: c.hostname, IP: c.ip, LastSeen: c.lastSeen,
		})
	}
	return out
}

// Connect 是双向流 RPC 的服务端实现, 每个 Agent 连上来都走这个方法。
//
// 协议流程:
//   1. Agent 建连后第一条消息必须是 Register(上报 id/hostname/ip)
//   2. 控制面回复 RegisterAck(accepted=true)
//   3. 之后双方持续通信: Agent 发心跳/结果, 控制面发指令
//   4. 连接断开时从注册表移除该 Agent
//
// 为什么先 Recv 再启动 send goroutine:
//   Register 之前 conn 为 nil, send goroutine 无法工作。
//   等 Register 拿到 conn 后再启动, 避免 conn 变量的数据竞争。
func (s *Server) Connect(stream pb.AgentService_ConnectServer) error {
	// 第一条消息必须是 Register
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

	// send goroutine: 从 conn.send 通道读下行消息, 通过 stream 发给 Agent
	// 监听 ctx.Done(连接断开)和 conn.done(被新连接替换)两个退出信号
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

	// 主循环: 读 Agent 上行消息(心跳/结果), 直到连接断开
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

	// 连接断开: 清理注册表, 等 send goroutine 退出
	s.removeAgent(conn)
	<-sendDone
	return nil
}

// handleRegister 处理注册消息: 创建 agentConn 放入注册表, 回复 ACK。
// 如果同一 Agent 重连(旧连接还在), 关闭旧连接的 done 通道让它退出,
// 避免同一个 agent_id 出现两条活跃连接(重连竞态防护)。
func (s *Server) handleRegister(req *pb.RegisterRequest, stream pb.AgentService_ConnectServer) *agentConn {
	c := &agentConn{
		id:       req.AgentId,
		hostname: req.Hostname,
		ip:       req.Ip,
		lastSeen: time.Now(),
		send:     make(chan *pb.ServerMessage, 16), // 缓冲 16 条, 防止短暂阻塞丢指令
		done:     make(chan struct{}),
	}
	s.mu.Lock()
	if old, ok := s.agents[req.AgentId]; ok {
		close(old.done) // 通知旧连接的 send goroutine 退出
	}
	s.agents[req.AgentId] = c
	s.mu.Unlock()

	// 回复注册确认
	_ = stream.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_RegisterAck{
			RegisterAck: &pb.RegisterAck{Accepted: true, Message: "注册成功"},
		},
	})
	return c
}

// removeAgent 从注册表移除 Agent 连接。
// 用指针比较(cur == c)确保只删自己, 不误删重连后的新连接。
func (s *Server) removeAgent(c *agentConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.agents[c.id]; ok && cur == c {
		delete(s.agents, c.id)
		close(c.done)
		log.Printf("Agent %s 断开连接", c.id)
	}
}

// handleHeartbeat 更新 Agent 的最后心跳时间。
// 后续可在此处理 CPU/内存等指标(M4 之后的扩展点)。
func (s *Server) handleHeartbeat(c *agentConn, hb *pb.Heartbeat) {
	s.mu.Lock()
	c.lastSeen = time.Now()
	s.mu.Unlock()
}

// handleResult 把 Agent 回传的指令结果投递到等待通道。
// ReadConfig 下发指令后会阻塞在通道上等结果, 这里完成投递。
// 如果没有等待者(指令已超时被清理), 只记日志不报错。
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

// ReadConfig 向指定 Agent 下发"读取配置文件"指令, 阻塞等待结果。
//
// 流程:
//   1. 生成唯一 commandID, 创建结果等待通道
//   2. 通过 Agent 的 send 通道下发 READ_CONFIG 指令
//   3. 阻塞等待 Agent 回传结果(带超时)
//   4. defer 兜底清理: 无论成功失败都从 results map 移除通道
//
// 供 HTTP API 的 POST /api/agents/{id}/read-config 调用。
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
	// 兜底清理: 超时或完成后移除通道, 防止内存泄漏
	defer func() {
		s.mu.Lock()
		delete(s.results, cmdID)
		s.mu.Unlock()
	}()

	// 构造指令并下发
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

	// 等待 Agent 回传结果
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
// CollectConfigs 向指定 Agent 下发配置采集指令, 阻塞等待结果(带超时)。
// 供 HTTP API 调用。返回 Agent 回传的采集快照 JSON。
func (s *Server) CollectConfigs(agentID string, params map[string]string, timeout time.Duration) (string, error) {
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
	defer func() {
		s.mu.Lock()
		delete(s.results, cmdID)
		s.mu.Unlock()
	}()

	cmd := &pb.Command{
		CommandId: cmdID,
		Type:      "COLLECT_CONFIGS",
		Params:    params,
	}
	select {
	case c.send <- &pb.ServerMessage{Payload: &pb.ServerMessage_Command{Command: cmd}}:
	case <-c.done:
		return "", fmt.Errorf("agent %s 已断开", agentID)
	case <-time.After(timeout):
		return "", fmt.Errorf("下发采集指令超时")
	}

	select {
	case r := <-ch:
		if !r.Success {
			return "", fmt.Errorf("agent 执行失败: %s", r.Error)
		}
		return r.Output, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("等待采集结果超时")
	}
}