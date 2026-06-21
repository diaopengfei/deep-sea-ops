package main

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/hashicorp/raft"
)

// FSM 是状态机。Raft 负责把命令按顺序可靠地送达, FSM 负责收到命令后真正改状态。
// 必须实现 raft.FSM 接口的三个方法: Apply / Snapshot / Restore。
type FSM struct {
	mu      sync.RWMutex      // 保护下面的 map, 因为 Apply 和 List 可能并发
	servers map[string]Server // 内存存储, key 是 Server.ID
}

func NewFSM() *FSM {
	return &FSM{servers: make(map[string]Server)}
}

// Apply 在 Raft 确认一条命令后被回调。l.Data 就是我们传给 raft.Apply 的字节。
// 把字节反序列化成 command, 然后改 map。
// 注意参数名用 l 而不是 log, 否则会遮蔽 log 包导致 log.Printf 编译失败。
func (f *FSM) Apply(l *raft.Log) interface{} {
	var cmd command
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		return fmt.Errorf("反序列化命令失败: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch cmd.Op {
	case "add":
		f.servers[cmd.Server.ID] = cmd.Server
		log.Printf("FSM 应用 add 命令: id=%s name=%s", cmd.Server.ID, cmd.Server.Name)
		return nil
	default:
		return fmt.Errorf("未知操作: %s", cmd.Op)
	}
}

// List 是业务方法, 供 API 读取当前状态。读直接走内存, 不过 Raft。
func (f *FSM) List() []Server {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]Server, 0, len(f.servers))
	for _, s := range f.servers {
		out = append(out, s)
	}
	return out
}

// --- Snapshot / Restore ---

type snapshotData struct {
	Servers map[string]Server
}

// Snapshot 打包当前状态, 供 Raft 压缩日志和给新节点同步用。
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	clone := make(map[string]Server, len(f.servers))
	for k, v := range f.servers {
		clone[k] = v
	}
	return &fsmSnapshot{data: snapshotData{Servers: clone}}, nil
}

// Restore 在节点启动时从快照恢复(在重放日志之前)。
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var data snapshotData
	if err := gob.NewDecoder(rc).Decode(&data); err != nil {
		return fmt.Errorf("恢复快照失败: %w", err)
	}
	f.mu.Lock()
	f.servers = data.Servers
	f.mu.Unlock()
	log.Printf("FSM 从快照恢复: %d 台服务器", len(data.Servers))
	return nil
}

type fsmSnapshot struct {
	data snapshotData
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if err := gob.NewEncoder(sink).Encode(s.data); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}