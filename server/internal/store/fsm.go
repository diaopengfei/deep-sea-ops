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
// 一个 bbolt 文件里可以有多个 bucket, 互不干扰。
var (
	serversBucket = []byte("servers") // 服务器清单
	usersBucket   = []byte("users")   // 用户账户(登录鉴权)
)

// FSM 是状态机。Raft 负责把命令按顺序可靠地送达, FSM 负责收到命令后真正改状态。
// 必须实现 raft.FSM 接口的三个方法: Apply / Snapshot / Restore。
//
// M2 起: 内部存储用 bbolt(嵌入式 KV 数据库, 单文件)。
// 好处: 1) bbolt 自带事务锁, 不再需要 sync.RWMutex
//       2) 数据持久化在文件里, 大数据量下读写按需进行
// v0.3 起: 增加 users bucket, 用户数据同样走 Raft 保证多节点一致。
// 对外接口(Apply/Snapshot/Restore 签名)完全不变, 上层无感知。
type FSM struct {
	db *bbolt.DB
}

// NewFSM 打开(或创建)bbolt 文件, 并确保 servers/users 桶存在。
// dbPath 是 bbolt 数据库文件的完整路径。
func NewFSM(dbPath string) (*FSM, error) {
	// bbolt.Open 要求父目录存在, 这里其实已由上层 NewStore 的 MkdirAll 保证,
	// 但防御性检查一下, 避免"路径是文件而非目录"这类边界情况。
	db, err := bbolt.Open(filepath.Join(filepath.Dir(dbPath), filepath.Base(dbPath)), 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("打开 bbolt: %w", err)
	}
	// 初始化时创建 bucket(已存在则跳过)。Update 是写事务, 自动提交。
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, b := range [][]byte{serversBucket, usersBucket} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("创建 bucket: %w", err)
	}
	return &FSM{db: db}, nil
}

// Close 关闭底层 bbolt 数据库, 释放文件锁。
func (f *FSM) Close() error {
	return f.db.Close()
}

// Apply 在 Raft 确认一条命令后被回调。l.Data 就是我们传给 raft.Apply 的字节。
// 用 bbolt 写事务把数据落盘。
// 注意参数名用 l 而不是 log, 否则会遮蔽 log 包导致 log.Printf 编译失败。
func (f *FSM) Apply(l *raft.Log) interface{} {
	var cmd command
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		return fmt.Errorf("反序列化命令失败: %w", err)
	}

	// bbolt 的写事务: 事务返回 nil 则提交, 返回 error 则回滚。
	// 写事务天然串行(bbolt 内部锁), 所以不用自己加锁。
	err := f.db.Update(func(tx *bbolt.Tx) error {
		switch cmd.Op {
		case "add_server":
			return f.applyAddServer(tx, cmd.Server)
		case "add_user":
			return f.applyAddUser(tx, cmd.User)
		default:
			return fmt.Errorf("未知操作: %s", cmd.Op)
		}
	})
	if err != nil {
		log.Printf("FSM Apply 失败: %v", err)
		return err
	}
	log.Printf("FSM 应用 %s 命令", cmd.Op)
	return nil
}

// applyAddServer 把一台服务器写入 servers bucket。
func (f *FSM) applyAddServer(tx *bbolt.Tx, srv model.Server) error {
	b := tx.Bucket(serversBucket)
	val, err := json.Marshal(srv)
	if err != nil {
		return err
	}
	return b.Put([]byte(srv.ID), val)
}

// applyAddUser 把一个用户写入 users bucket。
func (f *FSM) applyAddUser(tx *bbolt.Tx, u model.User) error {
	b := tx.Bucket(usersBucket)
	val, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return b.Put([]byte(u.Username), val)
}

// --- 服务器读取 ---

// List 是业务方法, 供 API 读取当前所有服务器。用读事务遍历 bucket。
func (f *FSM) List() []model.Server {
	var out []model.Server
	f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(serversBucket)
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

// --- 用户读取 ---

// GetUser 按用户名查用户, 供登录鉴权使用。
// 返回 (用户指针, 是否找到)。未找到时返回 (nil, false)。
func (f *FSM) GetUser(username string) (*model.User, bool) {
	var u *model.User
	f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		val := b.Get([]byte(username))
		if val == nil {
			return nil
		}
		u = &model.User{}
		return json.Unmarshal(val, u)
	})
	if u == nil {
		return nil, false
	}
	return u, true
}

// ListUsers 列出所有用户(供管理界面, 后续版本)。
func (f *FSM) ListUsers() []model.User {
	var out []model.User
	f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		return b.ForEach(func(k, v []byte) error {
			var u model.User
			if err := json.Unmarshal(v, &u); err != nil {
				return err
			}
			out = append(out, u)
			return nil
		})
	})
	return out
}

// --- Snapshot / Restore ---

type snapshotData struct {
	Servers map[string]model.Server
	Users   map[string]model.User
}

// Snapshot 打包当前状态, 供 Raft 压缩日志和给新节点同步用。
// 从 bbolt 读出所有 KV, 转成 map 打包。
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	servers := make(map[string]model.Server)
	users := make(map[string]model.User)
	f.db.View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket(serversBucket); b != nil {
			b.ForEach(func(k, v []byte) error {
				var s model.Server
				if err := json.Unmarshal(v, &s); err != nil {
					return err
				}
				servers[string(k)] = s
				return nil
			})
		}
		if b := tx.Bucket(usersBucket); b != nil {
			b.ForEach(func(k, v []byte) error {
				var u model.User
				if err := json.Unmarshal(v, &u); err != nil {
					return err
				}
				users[string(k)] = u
				return nil
			})
		}
		return nil
	})
	return &fsmSnapshot{data: snapshotData{Servers: servers, Users: users}}, nil
}

// Restore 在节点启动时从快照恢复(在重放日志之前)。
// 清空所有 bucket, 再把快照内容写回去, 保证状态与快照一致。
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var data snapshotData
	if err := gob.NewDecoder(rc).Decode(&data); err != nil {
		return fmt.Errorf("恢复快照失败: %w", err)
	}
	if err := f.db.Update(func(tx *bbolt.Tx) error {
		// 逐个 bucket: 删旧重建, 比 ForEach+Delete 干净。
		for _, name := range [][]byte{serversBucket, usersBucket} {
			if err := tx.DeleteBucket(name); err != nil && err != bbolt.ErrBucketNotFound {
				return err
			}
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		// 写回服务器
		sb := tx.Bucket(serversBucket)
		for id, s := range data.Servers {
			val, err := json.Marshal(s)
			if err != nil {
				return err
			}
			if err := sb.Put([]byte(id), val); err != nil {
				return err
			}
		}
		// 写回用户
		ub := tx.Bucket(usersBucket)
		for name, u := range data.Users {
			val, err := json.Marshal(u)
			if err != nil {
				return err
			}
			if err := ub.Put([]byte(name), val); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("写入恢复数据: %w", err)
	}
	log.Printf("FSM 从快照恢复: %d 台服务器, %d 个用户", len(data.Servers), len(data.Users))
	return nil
}

type fsmSnapshot struct {
	data snapshotData
}

// Persist 把快照数据写入 Raft 提供的 sink。
func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if err := gob.NewEncoder(sink).Encode(s.data); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

// Release 释放快照资源(Raft 接口要求, 这里无额外资源)。
func (s *fsmSnapshot) Release() {}