package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"

	"github.com/deepsea-ops/server/internal/model"
)

// Store 封装 Raft 实例, 对外提供业务层读写方法。
// 上层(api/auth)只跟 Store 打交道, 不直接碰 raft.Raft 细节。
//
// 设计要点:
//   - 写操作(AddServer/AddUser)必须经 raft.Apply, 保证多节点一致
//   - 读操作(ListServers/GetUser)直接读 FSM(bbolt), 不走 Raft, 读多写少时性能好
//   - 错误路径用 closer 列表逆序释放已打开资源, 避免泄漏
type Store struct {
	raft   *raft.Raft
	fsm    *FSM
	nodeID string
	closer []func() error // 出错时逆序关闭的资源
}

// NewStore 创建并启动一个 Raft 节点。
//
//   raftDir  : Raft 日志/快照目录
//   nodeID   : 本节点唯一 ID(如 node1/node2/node3)
//   raftAddr : 本节点 Raft 通信地址(如 127.0.0.1:7001)
//   joinAddr : 已有集群 Leader 的 Raft 地址; 为空表示首个节点(自己 bootstrap)
//
// 首个节点: 直接 BootstrapCluster 注册自己为唯一 voter。
// 加入节点: 不 bootstrap, 保持 Follower 等待 Leader AddVoter 纳入。
func NewStore(raftDir, nodeID, raftAddr, joinAddr string) (*Store, error) {
	if err := os.MkdirAll(raftDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 raft 目录: %w", err)
	}

	s := &Store{nodeID: nodeID}

	// FSM 用 bbolt 持久化, 存 servers 和 users 两个 bucket
	fsm, err := NewFSM(filepath.Join(raftDir, "fsm.db"))
	if err != nil {
		return nil, fmt.Errorf("创建 FSM: %w", err)
	}
	s.fsm = fsm
	s.closer = append(s.closer, fsm.Close)

	// cleanup 在出错时逆序关闭已打开资源
	cleanup := func() {
		for i := len(s.closer) - 1; i >= 0; i-- {
			_ = s.closer[i]()
		}
	}

	// --- Raft 配置 ---
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(nodeID) // 多节点必须各自不同
	config.SnapshotInterval = 30 * time.Second
	config.SnapshotThreshold = 2

	// BoltStore: Raft 用它存命令日志(stable store + log store)
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(raftDir, "raft.db"))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 bolt store: %w", err)
	}
	s.closer = append(s.closer, logStore.Close)

	// FileSnapshotStore: Raft 用它存快照文件, 保留 1 份
	snapshotStore, err := raft.NewFileSnapshotStore(raftDir, 1, os.Stderr)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 snapshot store: %w", err)
	}

	// TCPTransport: Raft 节点间通信, 3 个连接池, 10s 超时
	transport, err := raft.NewTCPTransport(raftAddr, nil, 3, 10*time.Second, os.Stderr)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 transport: %w", err)
	}
	s.closer = append(s.closer, transport.Close)

	// 组装 Raft 实例
	r, err := raft.NewRaft(config, fsm, logStore, logStore, snapshotStore, transport)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 raft: %w", err)
	}

	if joinAddr == "" {
		// 首个节点: bootstrap 自己为唯一 voter
		bootstrapCfg := raft.Configuration{
			Servers: []raft.Server{{
				ID:      config.LocalID,
				Address: transport.LocalAddr(),
			}},
		}
		if err := r.BootstrapCluster(bootstrapCfg).Error(); err != nil {
			// ErrCantBootstrap 表示集群已存在(重启场景), 正常忽略
			if !errors.Is(err, raft.ErrCantBootstrap) {
				cleanup()
				return nil, fmt.Errorf("bootstrap: %w", err)
			}
		}
	}

	s.raft = r
	if joinAddr != "" {
		// 加入节点: 不 bootstrap, 等 Leader 通过 AddVoter 纳入
		// 此时本节点是 Follower, 会在被加入后自动同步日志
		log.Printf("Raft 节点启动(Follower 待加入), id=%s addr=%s, 等待 Leader %s 纳入", nodeID, raftAddr, joinAddr)
	} else {
		// 首个节点: 等自己当选 Leader
		if err := s.waitForLeader(10 * time.Second); err != nil {
			cleanup()
			return nil, err
		}
		log.Printf("Raft 单节点就绪, Leader=%s", raftAddr)
	}
	return s, nil
}

// waitForLeader 轮询直到本节点成为 Leader(首节点启动用)。
func (s *Store) waitForLeader(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.raft.State() == raft.Leader {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("等待 Leader 超时")
}

// --- 服务器相关 ---

// AddServer 提交"新增服务器"命令到 Raft, 返回 FSM 分配的自增 ID。
func (s *Store) AddServer(srv model.Server) (int64, error) {
	cmd := command{Op: opAddServer, Server: srv}
	data, err := json.Marshal(cmd)
	if err != nil {
		return 0, fmt.Errorf("序列化命令: %w", err)
	}
	f := s.raft.Apply(data, 5*time.Second)
	if err := f.Error(); err != nil {
		return 0, fmt.Errorf("apply 命令: %w", err)
	}
	// FSM.Apply 返回分配的 ID 或 error
	if resp := f.Response(); resp != nil {
		if e, ok := resp.(error); ok {
			return 0, e
		}
		if id, ok := resp.(int64); ok {
			return id, nil
		}
	}
	return 0, nil
}

// UpdServerFields 提交"原子部分更新服务器"命令到 Raft(解决读-改-写竞态)。
// FSM 在同一个事务中读取现有记录并合并非零值字段, 整个操作原子完成。
func (s *Store) UpdServerFields(upd *ServerUpdate) error {
	cmd := command{Op: opUpdServerFields, ServerUpd: upd}
	return s.apply(cmd)
}

// DelServer 提交"删除服务器"命令到 Raft。
func (s *Store) DelServer(id string) error {
	cmd := command{Op: opDelServer, ServerID: id}
	return s.apply(cmd)
}

// ListServers 读取所有服务器(读不过 Raft, 直接读 bbolt)。
func (s *Store) ListServers() []model.Server {
	return s.fsm.List()
}

// GetServer 按 ID 查单个服务器。
func (s *Store) GetServer(id int64) (*model.Server, bool) {
	return s.fsm.GetServer(id)
}

// --- 用户相关 ---

// AddUser 提交"新增用户"命令到 Raft。
func (s *Store) AddUser(u model.User) error {
	cmd := command{Op: opAddUser, User: u}
	return s.apply(cmd)
}

// GetUser 按用户名查用户(登录校验用)。
func (s *Store) GetUser(username string) (*model.User, bool) {
	return s.fsm.GetUser(username)
}

// ListUsers 列出所有用户(管理用)。
func (s *Store) ListUsers() []model.User {
	return s.fsm.ListUsers()
}

// UpdateUser 提交"修改用户"命令到 Raft (v0.6.9)。
// u.Username 为定位键;PasswordHash 非空改密码;Role 非空改角色。
func (s *Store) UpdateUser(u model.User) error {
	cmd := command{Op: opUpdUser, User: u}
	return s.apply(cmd)
}

// DeleteUser 提交"删除用户"命令到 Raft (v0.6.9)。
func (s *Store) DeleteUser(username string) error {
	cmd := command{Op: opDelUser, User: model.User{Username: username}}
	return s.apply(cmd)
}

// --- 项目相关 ---

// AddProject 提交"新增项目记录"命令到 Raft。
func (s *Store) AddProject(p model.ProjectRecord) error {
	cmd := command{Op: opAddProject, Project: p}
	return s.apply(cmd)
}

// ClearAgentProjects 提交"清除指定 Agent 的所有项目"命令到 Raft(重新扫描前清旧数据)。
func (s *Store) ClearAgentProjects(agentID string) error {
	cmd := command{Op: opClearAgentProjects, Project: model.ProjectRecord{AgentID: agentID}}
	return s.apply(cmd)
}

// ListProjects 列出所有项目记录, 可选按 agentID 过滤。
func (s *Store) ListProjects(agentID string) []model.ProjectRecord {
	return s.fsm.ListProjects(agentID)
}

// SetConfigDiff 提交"更新项目配置比对结果"命令到 Raft。
// 后台扫描调度器比对完成后调用, 结果持久化供前端查询。
func (s *Store) SetConfigDiff(upd *ConfigDiffUpdate) error {
	cmd := command{Op: opSetConfigDiff, ConfigDiff: upd}
	return s.apply(cmd)
}

// SetConfigBaseline 提交"保存配置基准版本"命令到 Raft(v0.6.5)。
// FSM 在同一事务中: 自增版本号 → 更新项目基准字段 → 追加版本历史。
// 返回 FSM 分配的新版本号。
func (s *Store) SetConfigBaseline(upd *ConfigBaselineUpdate) error {
	cmd := command{Op: opSetConfigBaseline, Baseline: upd}
	return s.apply(cmd)
}

// ListConfigVersions 列出指定项目的配置基准版本历史(按版本号升序)。
func (s *Store) ListConfigVersions(projectID string) []model.ConfigVersion {
	return s.fsm.ListConfigVersions(projectID)
}

// GetConfigVersion 按 项目ID+版本号 查单条版本历史(回滚时取目标版本内容)。
func (s *Store) GetConfigVersion(projectID string, version int) (*model.ConfigVersion, bool) {
	return s.fsm.GetConfigVersion(projectID, version)
}

// GetProject 按 ID 查单个项目。
func (s *Store) GetProject(id string) (*model.ProjectRecord, bool) {
	return s.fsm.GetProject(id)
}

// --- 部署任务相关 ---

// AddDeployTask 提交"新增部署任务"命令到 Raft。
func (s *Store) AddDeployTask(t model.DeployTask) error {
	cmd := command{Op: opAddDeployTask, Task: t}
	return s.apply(cmd)
}

// UpdDeployTask 更新部署任务状态(走 Raft 保证多节点一致)。
func (s *Store) UpdDeployTask(t model.DeployTask) error {
	cmd := command{Op: opUpdDeployTask, Task: t}
	return s.apply(cmd)
}

// ListDeployTasks 列出所有部署任务。
func (s *Store) ListDeployTasks() []model.DeployTask {
	return s.fsm.ListDeployTasks()
}

// --- SSH 凭据相关 ---

// AddCredential 提交"新增 SSH 凭据"命令到 Raft。
func (s *Store) AddCredential(c model.SSHCredential) error {
	cmd := command{Op: opAddCredential, Credential: c}
	return s.apply(cmd)
}

// DelCredential 提交"删除 SSH 凭据"命令到 Raft。
func (s *Store) DelCredential(id string) error {
	cmd := command{Op: opDelCredential, CredID: id}
	return s.apply(cmd)
}

// GetCredential 按 ID 查 SSH 凭据。
func (s *Store) GetCredential(id string) (*model.SSHCredential, bool) {
	return s.fsm.GetCredential(id)
}

// ListCredentials 列出所有 SSH 凭据。
func (s *Store) ListCredentials() []model.SSHCredential {
	return s.fsm.ListCredentials()
}

// --- 集群管理 ---

// AddVoter 把一个新节点加入集群(Leader 调用)。
// nodeID/addr 是新节点的 Raft ID 和通信地址。
// 在 AddVoter 前做最终 voter 数量校验, 缩小 TOCTOU 窗口。
func (s *Store) AddVoter(nodeID, addr string) error {
	if s.raft.State() != raft.Leader {
		return errors.New("只有 Leader 能加节点")
	}
	// 最终校验: 获取最新集群配置, 检查加入后是否超范围
	future := s.raft.GetConfiguration()
	if err := future.Error(); err == nil {
		voterCount := 0
		for _, srv := range future.Configuration().Servers {
			if srv.Suffrage == raft.Voter {
				voterCount++
			}
		}
		if voterCount+1 > 7 {
			return fmt.Errorf("raft 集群 Voter 数量超限(当前 %d, 加入后 %d, 上限 7)", voterCount, voterCount+1)
		}
	}
	f := s.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 5*time.Second)
	return f.Error()
}

// ClusterInfo 返回集群状态信息(节点角色/Leader/成员列表)。
func (s *Store) ClusterInfo() ClusterInfo {
	future := s.raft.GetConfiguration()
	_ = future.Error()
	cfg := future.Configuration()

	servers := make([]ServerInfo, 0, len(cfg.Servers))
	for _, srv := range cfg.Servers {
		servers = append(servers, ServerInfo{
			ID:      string(srv.ID),
			Address: string(srv.Address),
			Suffrage: func() string {
				if srv.Suffrage == raft.Voter {
					return "Voter"
				}
				return "Nonvoter"
			}(),
		})
	}

	return ClusterInfo{
		ID:      s.nodeID,
		State:   s.raft.State().String(),
		Leader:  string(s.raft.Leader()),
		Term:    s.raft.Stats()["term"],
		Servers: servers,
	}
}

// Close 关闭 Store, 逆序释放资源(Raft/transport/bbolt)。
func (s *Store) Close() error {
	if s.raft != nil {
		_ = s.raft.Shutdown().Error()
	}
	for i := len(s.closer) - 1; i >= 0; i-- {
		_ = s.closer[i]()
	}
	return nil
}

// apply 是内部辅助: 把命令序列化后提交 Raft, 统一处理错误。
func (s *Store) apply(cmd command) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("序列化命令: %w", err)
	}
	f := s.raft.Apply(data, 5*time.Second)
	if err := f.Error(); err != nil {
		return fmt.Errorf("apply 命令: %w", err)
	}
	// FSM.Apply 返回的 error 表示业务层执行失败(如未知 op)
	if resp := f.Response(); resp != nil {
		if e, ok := resp.(error); ok {
			return e
		}
	}
	return nil
}

// ClusterInfo / ServerInfo 是给 API 层返回集群状态的 DTO。
type ClusterInfo struct {
	ID      string       `json:"id"`
	State   string       `json:"state"`
	Leader  string       `json:"leader"`
	Term    string       `json:"term"`
	Servers []ServerInfo `json:"servers"`
}

type ServerInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Suffrage string `json:"suffrage"`
}