package main

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
)

// Store 封装 Raft 实例, 对外提供简单的读写方法。
// 上层(API 层)只跟 Store 打交道, 不直接碰 raft.Raft 的细节。
type Store struct {
	raft *raft.Raft
	fsm  *FSM
}

// NewStore 创建并启动一个 Raft 单节点。
// raftDir: 存放 Raft 日志/快照的目录
// raftAddr: Raft 节点间通信地址(单节点就是自己)
func NewStore(raftDir, raftAddr string) (*Store, error) {
	// 1. 准备数据目录
	if err := os.MkdirAll(raftDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 raft 目录: %w", err)
	}

	// 2. 创建 FSM
	fsm := NewFSM()

	// 3. Raft 配置
	config := raft.DefaultConfig()
	config.LocalID = "node1"                  // 节点 ID, 单节点随便取
	config.SnapshotInterval = 30 * time.Second
	config.SnapshotThreshold = 2              // 积累 2 条日志就可能做快照(演示用, 生产环境调大)

	// 4. 日志存储 + 稳定存储, 都用 bbolt
	//    bbolt 文件就是 Raft 命令日志的持久化, 重启后靠它重放恢复 FSM
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(raftDir, "raft.db"))
	if err != nil {
		return nil, fmt.Errorf("创建 bolt store: %w", err)
	}

	// 5. 快照存储, 用文件系统
	snapshotStore, err := raft.NewFileSnapshotStore(raftDir, 1, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("创建 snapshot store: %w", err)
	}

	// 6. 传输层, TCP。节点间靠它复制日志/投票。单节点也会起这个监听。
	transport, err := raft.NewTCPTransport(raftAddr, nil, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("创建 transport: %w", err)
	}

	// 7. 组装 Raft 实例
	r, err := raft.NewRaft(config, fsm, logStore, logStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("创建 raft: %w", err)
	}

	// 8. 引导单节点集群。只有首次启动(没有已有集群)需要, 重启时已经引导过会返回错误, 忽略即可。
	bootstrapCfg := raft.Configuration{
		Servers: []raft.Server{{
			ID:      config.LocalID,
			Address: transport.LocalAddr(),
		}},
	}
	if err := r.BootstrapCluster(bootstrapCfg).Error(); err != nil {
		// ErrCantBootstrap 表示集群已存在(重启场景), 是正常的
		if !errors.Is(err, raft.ErrCantBootstrap) {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
	}

	store := &Store{raft: r, fsm: fsm}

	// 9. 等待自己成为 Leader(单节点几秒内就会当选), 否则写操作会失败
	if err := store.waitForLeader(10 * time.Second); err != nil {
		return nil, err
	}
	log.Printf("Raft 单节点就绪, Leader=%s", raftAddr)

	return store, nil
}

// waitForLeader 轮询直到本节点成为 Leader 或超时。
// Raft 写操作必须经过 Leader, 所以启动后要先等它当选。
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
// 这就是"写操作过 Raft"的完整路径。
func (s *Store) AddServer(srv Server) error {
	cmd := command{Op: "add", Server: srv}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("序列化命令: %w", err)
	}
	// Apply 返回一个 future, 5 秒超时。f.Error() 在命令被 commit 且 Apply 成功后返回 nil
	f := s.raft.Apply(data, 5*time.Second)
	if err := f.Error(); err != nil {
		return fmt.Errorf("apply 命令: %w", err)
	}
	// 如果 FSM.Apply 返回了错误, 会出现在 f.Response() 里
	if resp := f.Response(); resp != nil {
		if e, ok := resp.(error); ok {
			return e
		}
	}
	return nil
}

// ListServers 读取当前所有服务器。读操作直接走 FSM 内存, 不过 Raft。
func (s *Store) ListServers() []Server {
	return s.fsm.List()
}