package store

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"

	"github.com/hashicorp/raft"
	"go.etcd.io/bbolt"

	"github.com/deepsea-ops/server/internal/model"
)

// bucket 名字。bbolt 用"桶"组织 KV, 类似命名空间。
var serversBucket = []byte("servers")

// FSM 是状态机。Raft 负责把命令按顺序可靠地送达, FSM 负责收到命令后真正改状态。
// 必须实现 raft.FSM 接口的三个方法: Apply / Snapshot / Restore。
//
// M2 变化: 内部存储从内存 map 换成 bbolt(嵌入式 KV 数据库, 单文件)。
// 好处: 1) bbolt 自带事务锁, 不再需要 sync.RWMutex
//       2) 数据持久化在文件里, 大数据量下读写按需进行
// 对外接口(Apply/Snapshot/Restore 签名)完全不变, 上层无感知。
type FSM struct {
	db *bbolt.DB
}

// NewFSM 打开(或创建)bbolt 文件, 并确保 servers 桶存在。
func NewFSM(dbPath string) (*FSM, error) {
	// 确保父目录存在, 否则 bbolt.Open 会失败
	db, err := bbolt.Open(filepath.Join(dbPath), 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("打开 bbolt: %w", err)
	}
	// 初始化时创建 bucket(已存在则跳过)。Update 是写事务, 自动提交。
	if err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(serversBucket)
		return err
	}); err != nil {
		return nil, fmt.Errorf("创建 bucket: %w", err)
	}
	return &FSM{db: db}, nil
}

// Close 关闭底层 bbolt 数据库。
func (f *FSM) Close() error {
	return f.db.Close()
}

// Apply 在 Raft 确认一条命令后被回调。l.Data 就是我们传给 raft.Apply 的字节。
// 用 bbolt 写事务把数据落盘。
// 注意参数名用 l 而不是 log, 否则会遮蔽 log 包。
func (f *FSM) Apply(l *raft.Log) interface{} {
	var cmd command
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		return fmt.Errorf("反序列化命令失败: %w", err)
	}

	// bbolt 的写事务: 事务返回 nil 则提交, 返回 error 则回滚。
	// 写事务天然串行(bbolt 内部锁), 所以不用自己加锁。
	err := f.db.Update(func(tx *bbolt.Tx) error {
		switch cmd.Op {
		case "add":
			b := tx.Bucket(serversBucket)
			val, err := json.Marshal(cmd.Server)
			if err != nil {
				return err
			}
			// Put 的 key/value 都是 []byte。Put 天然幂等: 重复写同 key 结果不变。
			return b.Put([]byte(cmd.Server.ID), val)
		default:
			return fmt.Errorf("未知操作: %s", cmd.Op)
		}
	})
	if err != nil {
		log.Printf("FSM Apply 失败: %v", err)
		return err
	}
	log.Printf("FSM 应用 %s 命令: id=%s", cmd.Op, cmd.Server.ID)
	return nil
}

// List 是业务方法, 供 API 读取当前状态。用读事务遍历 bucket。
func (f *FSM) List() []model.Server {
	var out []model.Server
	// View 是只读事务, 不会阻塞写事务, 性能好。
	f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(serversBucket)
		// ForEach 遍历 bucket 里所有 KV, 返回非 nil 中止。
		return b.ForEach(func(k, v []byte) error {
			var s model.Server
			if err := json.Unmarshal(v, &s); err != nil {
				return err
			}
			out = append(out, s)
			return nil
		})
	})
	return out
}

// --- Snapshot / Restore ---

type snapshotData struct {
	Servers map[string]model.Server
}

// Snapshot 打包当前状态, 供 Raft 压缩日志和给新节点同步用。
// 从 bbolt 读出所有 KV, 转成 map 打包。
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	servers := make(map[string]model.Server)
	f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(serversBucket)
		return b.ForEach(func(k, v []byte) error {
			var s model.Server
			if err := json.Unmarshal(v, &s); err != nil {
				return err
			}
			servers[string(k)] = s
			return nil
		})
	})
	return &fsmSnapshot{data: snapshotData{Servers: servers}}, nil
}

// Restore 在节点启动时从快照恢复(在重放日志之前)。
// 清空当前 bucket, 再把快照内容写回去, 保证状态与快照一致。
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var data snapshotData
	if err := gob.NewDecoder(rc).Decode(&data); err != nil {
		return fmt.Errorf("恢复快照失败: %w", err)
	}
	// 删掉旧 bucket 重建, 比 ForEach+Delete 干净。
	if err := f.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.DeleteBucket(serversBucket); err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
		b, err := tx.CreateBucket(serversBucket)
		if err != nil {
			return err
		}
		for id, s := range data.Servers {
			val, err := json.Marshal(s)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(id), val); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("写入恢复数据: %w", err)
	}
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