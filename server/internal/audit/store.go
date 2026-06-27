// Package audit 实现操作审计日志的独立存储与查询。
//
// 设计要点:
//   - 独立 bbolt 文件(默认 raft-data/audit.db), 不进 Raft 状态机, 避免影响共识性能
//   - 追加写: 自增 ID 作 key(8 字节 big-endian, 天然有序), 只增不改不删
//   - 每个节点独立记录(入口代理把写请求转发到 Leader, 审计在处理节点本地记录)
//   - 查询走反向游标(最新优先), 内存过滤 + 分页; 审计量为运维操作级别, 全遍历可接受
//
// 审计日志记录: 操作人/时间/方法/路径/操作类型/目标/状态码/来源 IP/是否敏感。
package audit

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

var (
	auditBucket = []byte("audit_logs")
	metaBucket  = []byte("audit_meta")
	seqKey      = []byte("seq")
)

// Log 审计日志条目。
type Log struct {
	ID        int64  `json:"id"`        // 自增 ID(同时是 bbolt key)
	Timestamp int64  `json:"timestamp"` // unix 毫秒
	Username  string `json:"username"`  // 操作人(未登录的写操作如 login 为空)
	Role      string `json:"role"`      // 操作人角色
	Method    string `json:"method"`    // HTTP 方法
	Path      string `json:"path"`      // 请求路径
	Action    string `json:"action"`    // 操作类型: create-server/delete-server/inject/deploy 等
	Target    string `json:"target"`    // 操作目标 ID(如服务器 ID、Agent ID)
	Status    int    `json:"status"`    // 响应状态码
	IP        string `json:"ip"`        // 来源 IP
	Detail    string `json:"detail"`    // 备注
	Sensitive bool   `json:"sensitive"` // 是否敏感操作(删除/注入/停止进程等)
}

// Filter 审计日志查询过滤条件。字段为零值表示不过滤。
type Filter struct {
	Username string // 精确匹配操作人
	Action   string // 精确匹配操作类型
	Target   string // 精确匹配目标
	Start    int64  // 起始时间(unix 毫秒, 含)
	End      int64  // 结束时间(unix 毫秒, 含)
	Offset   int    // 分页偏移(从最新开始)
	Limit    int    // 每页数量, 0 表示不限
}

// Store 审计日志存储。线程安全(自增 ID 用互斥锁保护, bbolt 自带事务隔离)。
type Store struct {
	db  *bbolt.DB
	mu  sync.Mutex
	seq int64 // 内存自增计数器, 启动时从 db 恢复
}

// New 打开(或创建)审计日志 bbolt 文件并恢复自增计数器。
func New(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("打开审计日志库: %w", err)
	}
	s := &Store{db: db}
	if err := db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(auditBucket)
		if err != nil {
			return err
		}
		m, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return err
		}
		// 优先从 meta 恢复 seq; 兜底扫描 audit bucket 最大 key
		if v := m.Get(seqKey); v != nil {
			s.seq = int64(binary.BigEndian.Uint64(v))
		} else if k, _ := b.Cursor().Last(); k != nil {
			s.seq = int64(binary.BigEndian.Uint64(k))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s, nil
}

// Close 关闭底层 bbolt。
func (s *Store) Close() error { return s.db.Close() }

// Record 追加一条审计日志, 返回分配的 ID。
func (s *Store) Record(l Log) (int64, error) {
	s.mu.Lock()
	s.seq++
	id := s.seq
	s.mu.Unlock()

	l.ID = id
	if l.Timestamp == 0 {
		l.Timestamp = time.Now().UnixMilli()
	}
	data, err := json.Marshal(l)
	if err != nil {
		return 0, err
	}
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], uint64(id))
	if err := s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(auditBucket)
		if err := b.Put(key[:], data); err != nil {
			return err
		}
		return tx.Bucket(metaBucket).Put(seqKey, key[:])
	}); err != nil {
		return 0, err
	}
	return id, nil
}

// Query 反向遍历(最新优先), 按 filter 过滤, 返回当前页结果与匹配总数。
func (s *Store) Query(f Filter) ([]Log, int, error) {
	var result []Log
	total := 0
	skipped := 0
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(auditBucket).Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var l Log
			if err := json.Unmarshal(v, &l); err != nil {
				continue
			}
			if !match(l, f) {
				continue
			}
			total++
			if skipped < f.Offset {
				skipped++
				continue
			}
			if f.Limit > 0 && len(result) >= f.Limit {
				continue
			}
			result = append(result, l)
		}
		return nil
	})
	if result == nil {
		result = []Log{}
	}
	return result, total, err
}

// match 判断日志是否满足过滤条件。
func match(l Log, f Filter) bool {
	if f.Username != "" && l.Username != f.Username {
		return false
	}
	if f.Action != "" && l.Action != f.Action {
		return false
	}
	if f.Target != "" && l.Target != f.Target {
		return false
	}
	if f.Start > 0 && l.Timestamp < f.Start {
		return false
	}
	if f.End > 0 && l.Timestamp > f.End {
		return false
	}
	return true
}

// Purge 保留最近 keep 条, 删除更早的记录。keep<=0 不删除。
// 可在启动时调用一次控制日志体积。
func (s *Store) Purge(keep int) error {
	if keep <= 0 {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(auditBucket)
		c := b.Cursor()
		// 跳过最近 keep 条, 删除剩余
		count := 0
		var toDelete [][]byte
		for k, _ := c.Last(); k != nil; k, _ = c.Prev() {
			count++
			if count <= keep {
				continue
			}
			toDelete = append(toDelete, append([]byte(nil), k...))
		}
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}
