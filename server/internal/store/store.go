package store

import (
	"context"
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

// Store 封装 Raft 实例, 对外提供简单的读写方法。
type Store struct {
	raft   *raft.Raft
	fsm    *FSM
	nodeID string
	closer []func() error
}

// NewStore 创建并启动一个 Raft 节点。
//
//   raftDir  : Raft 日志/快照目录
//   nodeID   : 本节点唯一 ID(如 node1/node2/node3)
//   raftAddr : 本节点 Raft 通信地址(如 127.0.0.1:7001)
//   joinAddr : 已有集群 Leader 的 Raft 地址; 为空表示这是首个节点(自己 bootstrap)
//
// 首个节点: 直接 BootstrapCluster 注册自己为唯一 voter。
// 加入节点: 不 bootstrap, 启动后保持 Follower 等待被 Leader AddVoter 纳入。
func NewStore(raftDir, nodeID, raftAddr, joinAddr string) (*Store, error) {
	if err := os.MkdirAll(raftDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 raft 目录: %w", err)
	}

	s := &Store{nodeID: nodeID}

	fsm, err := NewFSM(filepath.Join(raftDir, "fsm.db"))
	if err != nil {
		return nil, fmt.Errorf("创建 FSM: %w", err)
	}
	s.fsm = fsm
	s.closer = append(s.closer, fsm.Close)

	cleanup := func() {
		for i := len(s.closer) - 1; i >= 0; i-- {
			_ = s.closer[i]()
		}
	}

	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(nodeID) // 多节点必须各自不同
	config.SnapshotInterval = 30 * time.Second
	config.SnapshotThreshold = 2

	logStore, err := raftboltdb.NewBoltStore(filepath.Join(raftDir, "raft.db"))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 bolt store: %w", err)
	}
	s.closer = append(s.closer, logStore.Close)

	snapshotStore, err := raft.NewFileSnapshotStore(raftDir, 1, os.Stderr)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 snapshot store: %w", err)
	}

	transport, err := raft.NewTCPTransport(raftAddr, nil, 3, 10*time.Second, os.Stderr)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 transport: %w", err)
	}
	s.closer = append(s.closer, transport.Close)

	r, err := raft.NewRaft(config, fsm, logStore, logStore, snapshotStore, transport)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("创建 raft: %w", err)
	}
	s.raft = r
	s.closer = append(s.closer, func() error { return r.Shutdown().Error() })

	if joinAddr == "" {
		// 首个节点: 引导单节点集群
		bootstrapCfg := raft.Configuration{
			Servers: []raft.Server{{
				ID:      config.LocalID,
				Address: transport.LocalAddr(),
			}},
		}
		if err := r.BootstrapCluster(bootstrapCfg).Error(); err != nil {
			if !errors.Is(err, raft.ErrCantBootstrap) {
				cleanup()
				return nil, fmt.Errorf("bootstrap: %w", err)
			}
		}
		// 首个节点等待自己当选 Leader
		if err := s.waitForLeader(10 * time.Second); err != nil {
			cleanup()
			return nil, err
		}
		log.Printf("Raft 首节点就绪(Leader), id=%s addr=%s", nodeID, raftAddr)
	} else {
		// 加入节点: 不 bootstrap, 等待被 Leader AddVoter 纳入后选举出 Leader
		// 这里不阻塞等待 Leader, 因为 AddVoter 由外部(HTTP join 接口)触发
		log.Printf("Raft 节点启动(Follower 待加入), id=%s addr=%s, 等待 Leader %s 纳入", nodeID, raftAddr, joinAddr)
	}

	return s, nil
}

// AddVoter 把一个新节点加入集群(仅 Leader 可调)。
// 供 HTTP /api/cluster/join 调用。
func (s *Store) AddVoter(nodeID, addr string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("当前节点不是 Leader, 无法添加 voter")
	}
	f := s.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 10*time.Second)
	if err := f.Error(); err != nil {
		return fmt.Errorf("AddVoter: %w", err)
	}
	log.Printf("已将节点 %s (%s) 加入集群", nodeID, addr)
	return nil
}

// ClusterInfo 返回集群状态(供 API/前端展示)。
type ClusterInfo struct {
	ID      string `json:"id"`
	State   string `json:"state"`   // Leader / Follower / Candidate
	Leader  string `json:"leader"`  // Leader 地址
	Term    uint64 `json:"term"`
	Servers []struct {
		ID      string `json:"id"`
		Address string `json:"address"`
		Suffrage string `json:"suffrage"` // Voter / Nonvoter
	} `json:"servers"`
}

func (s *Store) ClusterInfo() ClusterInfo {
	info := ClusterInfo{
		ID:    s.nodeID,
		State: s.raft.State().String(),
	}
	if leader := s.raft.Leader(); leader != "" {
		info.Leader = string(leader)
	}
	info.Term = parseTerm(s.raft.Stats()["term"])
	// 用 config 获取成员列表
	cfg := s.raft.GetConfiguration()
	if err := cfg.Error(); err == nil {
		for _, srv := range cfg.Configuration().Servers {
			info.Servers = append(info.Servers, struct {
				ID       string `json:"id"`
				Address  string `json:"address"`
				Suffrage string `json:"suffrage"`
			}{string(srv.ID), string(srv.Address), srv.Suffrage.String()})
		}
	}
	return info
}

// Close 释放所有资源(按打开逆序)。
func (s *Store) Close() error {
	var firstErr error
	for i := len(s.closer) - 1; i >= 0; i-- {
		if err := s.closer[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

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

// AddServer 提交一条"新增服务器"命令到 Raft。
func (s *Store) AddServer(srv model.Server) error {
	cmd := command{Op: "add", Server: srv}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("序列化命令: %w", err)
	}
	f := s.raft.Apply(data, 5*time.Second)
	if err := f.Error(); err != nil {
		return fmt.Errorf("apply 命令: %w", err)
	}
	if resp := f.Response(); resp != nil {
		if e, ok := resp.(error); ok {
			return e
		}
	}
	return nil
}

// ListServers 读取当前所有服务器。读操作直接走 FSM, 不过 Raft。
func (s *Store) ListServers() []model.Server {
	return s.fsm.List()
}

// 用 context 占位(后续 join 接口可能用)
var _ = context.Background

// parseTerm 把 Stats 里的 term 字符串转 uint64, 失败返回 0。
func parseTerm(s string) uint64 {
	var t uint64
	for _, ch := range s {
		if ch < '0' || ch > '9' { return 0 }
		t = t*10 + uint64(ch-'0')
	}
	return t
}
