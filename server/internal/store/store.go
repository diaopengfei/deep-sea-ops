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

// Store 封装 Raft 实例, 对外提供简单的读写方法。
// 上层(API 层)只跟 Store 打交道, 不直接碰 raft.Raft 的细节。
type Store struct {
	raft   *raft.Raft
	fsm    *FSM
	closer []func() error // 按打开逆序关闭的资源
}

// NewStore 创建并启动一个 Raft 单节点。
// raftDir: 存放 Raft 日志/快照 的目录
// raftAddr: Raft 节点间通信地址(单节点就是自己)
//
// 出错时已打开的资源会被关闭, 不泄漏。
func NewStore(raftDir, raftAddr string) (*Store, error) {
	if err := os.MkdirAll(raftDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 raft 目录: %w", err)
	}

	s := &Store{}

	// FSM 用 bbolt 持久化存储, 数据库文件放 raftDir 下。
	fsm, err := NewFSM(filepath.Join(raftDir, "fsm.db"))
	if err != nil {
		return nil, fmt.Errorf("创建 FSM: %w", err)
	}
	s.fsm = fsm
	s.closer = append(s.closer, fsm.Close)

	// 失败时按逆序关闭已打开资源
	cleanup := func() {
		for i := len(s.closer) - 1; i >= 0; i-- {
			_ = s.closer[i]()
		}
	}

	config := raft.DefaultConfig()
	config.LocalID = "node1"
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

	if err := s.waitForLeader(10 * time.Second); err != nil {
		cleanup()
		return nil, err
	}
	log.Printf("Raft 单节点就绪, Leader=%s", raftAddr)
	return s, nil
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
// 流程: 序列化命令 -> raft.Apply(一致性复制+commit) -> FSM.Apply 改状态 -> 返回
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