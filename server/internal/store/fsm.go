package store

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"time"

	"github.com/hashicorp/raft"
	"go.etcd.io/bbolt"

	"github.com/deepsea-ops/server/internal/model"
)

// bucket 名字。bbolt 用"桶"组织 KV, 类似命名空间。
// 一个 bbolt 文件里可以有多个 bucket, 互不干扰。
var (
	serversBucket        = []byte("servers")         // 服务器清单
	usersBucket          = []byte("users")           // 用户账户(登录鉴权)
	projectsBucket       = []byte("projects")        // 扫描到的项目(持久化)
	deployTasksBucket    = []byte("deploy_tasks")    // 部署任务(扩容迁移)
	credentialsBucket    = []byte("credentials")     // SSH 凭据
	configVersionsBucket = []byte("config_versions") // v0.6.5 配置基准版本历史
)

// FSM 是状态机。Raft 负责把命令按顺序可靠地送达, FSM 负责收到命令后真正改状态。
// 必须实现 raft.FSM 接口的三个方法: Apply / Snapshot / Restore。
type FSM struct {
	db *bbolt.DB
}

// NewFSM 打开(或创建)bbolt 文件, 并确保所有 bucket 存在。
func NewFSM(dbPath string) (*FSM, error) {
	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("打开 bbolt: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, b := range [][]byte{serversBucket, usersBucket, projectsBucket, deployTasksBucket, credentialsBucket, configVersionsBucket} {
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

// Apply 在 Raft 确认一条命令后被回调。
func (f *FSM) Apply(l *raft.Log) interface{} {
	var cmd command
	if err := json.Unmarshal(l.Data, &cmd); err != nil {
		return fmt.Errorf("反序列化命令失败: %w", err)
	}

	err := f.db.Update(func(tx *bbolt.Tx) error {
		switch cmd.Op {
		case opAddServer:
			return f.applyAddServer(tx, &cmd.Server)
		case opUpdServer:
			return f.applyUpdServer(tx, &cmd.Server)
		case opUpdServerFields:
			return f.applyUpdServerFields(tx, cmd.ServerUpd)
		case opDelServer:
			return f.applyDelServer(tx, cmd.ServerID)
		case opAddUser:
			return f.applyAddUser(tx, cmd.User)
		case opUpdUser:
			return f.applyUpdUser(tx, cmd.User) // v0.6.9 修改用户(改密码/角色)
		case opDelUser:
			return f.applyDelUser(tx, cmd.User.Username) // v0.6.9 删除用户
		case opAddProject:
			return f.applyAddProject(tx, cmd.Project)
		case opClearAgentProjects:
			return f.applyClearAgentProjects(tx, cmd.Project.AgentID)
		case opSetConfigDiff:
			return f.applySetConfigDiff(tx, cmd.ConfigDiff)
		case opSetConfigBaseline:
			return f.applySetConfigBaseline(tx, cmd.Baseline)
		case opAddDeployTask:
			return f.applyAddDeployTask(tx, cmd.Task)
		case opUpdDeployTask:
			return f.applyUpdDeployTask(tx, cmd.Task)
		case opAddCredential:
			return f.applyAddCredential(tx, cmd.Credential)
		case opDelCredential:
			return f.applyDelCredential(tx, cmd.CredID)
		default:
			return fmt.Errorf("未知操作: %s", cmd.Op)
		}
	})
	if err != nil {
		log.Printf("FSM Apply 失败: %v", err)
		return err
	}
	// add_server 返回分配的自增 ID, 供 API 层返回给调用方
	if cmd.Op == opAddServer {
		return cmd.Server.ID
	}
	return nil
}

// --- 服务器 ---

// applyAddServer 新增服务器。ID 为 0 时自动分配自增 ID(Raft 顺序执行, 无并发问题)。
func (f *FSM) applyAddServer(tx *bbolt.Tx, srv *model.Server) error {
	b := tx.Bucket(serversBucket)
	if srv.ID == 0 {
		// 自增 ID: 扫描现有最大 ID + 1
		maxID := int64(0)
		_ = b.ForEach(func(k, v []byte) error {
			var s model.Server
			if json.Unmarshal(v, &s) == nil && s.ID > maxID {
				maxID = s.ID
			}
			return nil
		})
		srv.ID = maxID + 1
	}
	val, err := json.Marshal(srv)
	if err != nil {
		return err
	}
	return b.Put([]byte(strconv.FormatInt(srv.ID, 10)), val)
}

// applyUpdServer 更新服务器(覆盖写)。
func (f *FSM) applyUpdServer(tx *bbolt.Tx, srv *model.Server) error {
	b := tx.Bucket(serversBucket)
	val, err := json.Marshal(srv)
	if err != nil {
		return err
	}
	return b.Put([]byte(strconv.FormatInt(srv.ID, 10)), val)
}

// applyUpdServerFields 原子部分更新服务器(解决读-改-写竞态)。
// 在同一个 bbolt 事务中读取现有记录, 只更新非零值字段, 整个操作原子完成。
func (f *FSM) applyUpdServerFields(tx *bbolt.Tx, upd *ServerUpdate) error {
	if upd == nil {
		return fmt.Errorf("ServerUpdate 为空")
	}
	b := tx.Bucket(serversBucket)
	key := []byte(strconv.FormatInt(upd.ID, 10))
	val := b.Get(key)
	if val == nil {
		return fmt.Errorf("服务器不存在: ID=%d", upd.ID)
	}
	var srv model.Server
	if err := json.Unmarshal(val, &srv); err != nil {
		return fmt.Errorf("反序列化服务器失败: %w", err)
	}
	// 只更新非零值字段
	if upd.Name != "" {
		srv.Name = upd.Name
	}
	if upd.IP != "" {
		srv.IP = upd.IP
	}
	if upd.Port != 0 {
		srv.Port = upd.Port
	}
	if upd.OS != "" {
		srv.OS = upd.OS
	}
	if upd.Username != "" {
		srv.Username = upd.Username
	}
	if upd.Password != "" {
		srv.Password = upd.Password
	}
	if upd.Status != "" {
		srv.Status = upd.Status
	}
	out, err := json.Marshal(srv)
	if err != nil {
		return err
	}
	return b.Put(key, out)
}

// applyDelServer 按 ID 删除服务器。
func (f *FSM) applyDelServer(tx *bbolt.Tx, id string) error {
	b := tx.Bucket(serversBucket)
	return b.Delete([]byte(id))
}

func (f *FSM) List() []model.Server {
	out := make([]model.Server, 0)
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(serversBucket)
		return b.ForEach(func(k, v []byte) error {
			var s model.Server
			if err := json.Unmarshal(v, &s); err != nil {
				return err
			}
			out = append(out, s)
			return nil
		})
	}); err != nil {
		log.Printf("FSM List 读取失败: %v", err)
	}
	return out
}

// GetServer 按 ID 查单个服务器。
func (f *FSM) GetServer(id int64) (*model.Server, bool) {
	var srv *model.Server
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(serversBucket)
		val := b.Get([]byte(strconv.FormatInt(id, 10)))
		if val == nil {
			return nil
		}
		srv = &model.Server{}
		return json.Unmarshal(val, srv)
	}); err != nil {
		log.Printf("FSM GetServer 读取失败: %v", err)
		return nil, false
	}
	if srv == nil {
		return nil, false
	}
	return srv, true
}

// --- 用户 ---

func (f *FSM) applyAddUser(tx *bbolt.Tx, u model.User) error {
	b := tx.Bucket(usersBucket)
	val, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return b.Put([]byte(u.Username), val)
}

func (f *FSM) GetUser(username string) (*model.User, bool) {
	var u *model.User
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		val := b.Get([]byte(username))
		if val == nil {
			return nil
		}
		u = &model.User{}
		return json.Unmarshal(val, u)
	}); err != nil {
		log.Printf("FSM GetUser 读取失败: %v", err)
		return nil, false
	}
	if u == nil {
		return nil, false
	}
	return u, true
}

func (f *FSM) ListUsers() []model.User {
	var out = make([]model.User, 0)
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(usersBucket)
		return b.ForEach(func(k, v []byte) error {
			var u model.User
			if err := json.Unmarshal(v, &u); err != nil {
				return err
			}
			out = append(out, u)
			return nil
		})
	}); err != nil {
		log.Printf("FSM ListUsers 读取失败: %v", err)
	}
	return out
}

// applyUpdUser 修改用户(v0.6.9)。
// cmd.User 中:Username 为定位键;PasswordHash 非空表示改密码;Role 非空表示改角色。
// 其余字段(ID/CreatedAt)保持原值不变。
func (f *FSM) applyUpdUser(tx *bbolt.Tx, u model.User) error {
	b := tx.Bucket(usersBucket)
	val := b.Get([]byte(u.Username))
	if val == nil {
		return fmt.Errorf("用户不存在: %s", u.Username)
	}
	var existing model.User
	if err := json.Unmarshal(val, &existing); err != nil {
		return err
	}
	if u.PasswordHash != "" {
		existing.PasswordHash = u.PasswordHash
	}
	if u.Role != "" {
		existing.Role = u.Role
	}
	out, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	return b.Put([]byte(u.Username), out)
}

// applyDelUser 删除用户(v0.6.9)。
func (f *FSM) applyDelUser(tx *bbolt.Tx, username string) error {
	b := tx.Bucket(usersBucket)
	return b.Delete([]byte(username))
}

// --- 项目 ---

func (f *FSM) applyAddProject(tx *bbolt.Tx, p model.ProjectRecord) error {
	b := tx.Bucket(projectsBucket)
	val, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return b.Put([]byte(p.ID), val)
}

// applyClearAgentProjects 清除指定 Agent 的所有项目记录(重新扫描前先清旧数据)。
func (f *FSM) applyClearAgentProjects(tx *bbolt.Tx, agentID string) error {
	b := tx.Bucket(projectsBucket)
	var keysToDelete [][]string
	b.ForEach(func(k, v []byte) error {
		var p model.ProjectRecord
		if err := json.Unmarshal(v, &p); err == nil {
			if p.AgentID == agentID {
				keysToDelete = append(keysToDelete, []string{string(k)})
			}
		} else {
			log.Printf("[警告] applyClearAgentProjects 反序列化项目失败: key=%s err=%v", string(k), err)
		}
		return nil
	})
	for _, k := range keysToDelete {
		if err := b.Delete([]byte(k[0])); err != nil {
			log.Printf("[警告] applyClearAgentProjects 删除项目失败: key=%s err=%v", k[0], err)
		}
	}
	return nil
}

// applySetConfigDiff 原子更新项目的配置比对结果。
// 在同一个 bbolt 事务中读取现有项目记录, 设置 ConfigDiffJSON 和 DiffScannedAt, 原子写回。
func (f *FSM) applySetConfigDiff(tx *bbolt.Tx, upd *ConfigDiffUpdate) error {
	if upd == nil {
		return fmt.Errorf("ConfigDiffUpdate 为空")
	}
	b := tx.Bucket(projectsBucket)
	key := []byte(upd.ProjectID)
	val := b.Get(key)
	if val == nil {
		return fmt.Errorf("项目不存在: ID=%s", upd.ProjectID)
	}
	var p model.ProjectRecord
	if err := json.Unmarshal(val, &p); err != nil {
		return fmt.Errorf("反序列化项目失败: %w", err)
	}
	p.ConfigDiffJSON = upd.ConfigDiff
	p.DiffScannedAt = upd.DiffScannedAt
	out, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return b.Put(key, out)
}

// applySetConfigBaseline 原子更新项目的配置基准版本并追加一条版本历史(v0.6.5)。
// 在同一个 bbolt 事务中: 读取现有项目 → 自增版本号 → 更新基准字段 → 写回项目 → 追加 ConfigVersion 到历史 bucket。
// key 设计: projectID + "|" + version, 同一项目的版本历史天然按 key 前缀聚簇且有序。
func (f *FSM) applySetConfigBaseline(tx *bbolt.Tx, upd *ConfigBaselineUpdate) error {
	if upd == nil {
		return fmt.Errorf("ConfigBaselineUpdate 为空")
	}
	pb := tx.Bucket(projectsBucket)
	key := []byte(upd.ProjectID)
	val := pb.Get(key)
	if val == nil {
		return fmt.Errorf("项目不存在: ID=%s", upd.ProjectID)
	}
	var p model.ProjectRecord
	if err := json.Unmarshal(val, &p); err != nil {
		return fmt.Errorf("反序列化项目失败: %w", err)
	}
	// 自增版本号, 0 表示尚未建立基准则从 1 开始
	p.BaselineVersion++
	if p.BaselineVersion <= 0 {
		p.BaselineVersion = 1
	}
	p.ConfigBaseline = upd.Content
	p.BaselineUpdatedAt = time.Now().UnixMilli()
	p.BaselineUpdatedBy = upd.UpdatedBy
	out, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := pb.Put(key, out); err != nil {
		return err
	}
	// 追加版本历史快照
	cv := model.ConfigVersion{
		ProjectID: upd.ProjectID,
		Version:   p.BaselineVersion,
		Content:   upd.Content,
		UpdatedBy: upd.UpdatedBy,
		UpdatedAt: p.BaselineUpdatedAt,
		Comment:   upd.Comment,
	}
	cb := tx.Bucket(configVersionsBucket)
	cvVal, err := json.Marshal(cv)
	if err != nil {
		return err
	}
	cvKey := upd.ProjectID + "|" + strconv.Itoa(cv.Version)
	return cb.Put([]byte(cvKey), cvVal)
}

// ListConfigVersions 列出指定项目的配置基准版本历史(按版本号升序)。
func (f *FSM) ListConfigVersions(projectID string) []model.ConfigVersion {
	out := make([]model.ConfigVersion, 0)
	prefix := []byte(projectID + "|")
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(configVersionsBucket)
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var cv model.ConfigVersion
			if err := json.Unmarshal(v, &cv); err == nil {
				out = append(out, cv)
			}
		}
		return nil
	}); err != nil {
		log.Printf("FSM ListConfigVersions 读取失败: %v", err)
	}
	return out
}

// GetConfigVersion 按 项目ID+版本号 查单条版本历史(回滚时取目标版本内容)。
func (f *FSM) GetConfigVersion(projectID string, version int) (*model.ConfigVersion, bool) {
	var cv *model.ConfigVersion
	key := []byte(projectID + "|" + strconv.Itoa(version))
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(configVersionsBucket)
		val := b.Get(key)
		if val == nil {
			return nil
		}
		cv = &model.ConfigVersion{}
		return json.Unmarshal(val, cv)
	}); err != nil {
		log.Printf("FSM GetConfigVersion 读取失败: %v", err)
		return nil, false
	}
	if cv == nil {
		return nil, false
	}
	return cv, true
}

// ListProjects 列出所有项目记录。可选按 agentID 过滤。
func (f *FSM) ListProjects(agentID string) []model.ProjectRecord {
	out := make([]model.ProjectRecord, 0)
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(projectsBucket)
		return b.ForEach(func(k, v []byte) error {
			var p model.ProjectRecord
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			if agentID != "" && p.AgentID != agentID {
				return nil
			}
			out = append(out, p)
			return nil
		})
	}); err != nil {
		log.Printf("FSM ListProjects 读取失败: %v", err)
	}
	return out
}

// GetProject 按 ID 查单个项目。
func (f *FSM) GetProject(id string) (*model.ProjectRecord, bool) {
	var p *model.ProjectRecord
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(projectsBucket)
		val := b.Get([]byte(id))
		if val == nil {
			return nil
		}
		p = &model.ProjectRecord{}
		return json.Unmarshal(val, p)
	}); err != nil {
		log.Printf("FSM GetProject 读取失败: %v", err)
		return nil, false
	}
	if p == nil {
		return nil, false
	}
	return p, true
}

// --- 部署任务 ---

func (f *FSM) applyAddDeployTask(tx *bbolt.Tx, t model.DeployTask) error {
	b := tx.Bucket(deployTasksBucket)
	val, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return b.Put([]byte(t.ID), val)
}

func (f *FSM) applyUpdDeployTask(tx *bbolt.Tx, t model.DeployTask) error {
	return f.applyAddDeployTask(tx, t) // 同样是 Put, 覆盖
}

func (f *FSM) ListDeployTasks() []model.DeployTask {
	out := make([]model.DeployTask, 0)
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(deployTasksBucket)
		return b.ForEach(func(k, v []byte) error {
			var t model.DeployTask
			if err := json.Unmarshal(v, &t); err != nil {
				return err
			}
			out = append(out, t)
			return nil
		})
	}); err != nil {
		log.Printf("FSM ListDeployTasks 读取失败: %v", err)
	}
	return out
}

func (f *FSM) GetDeployTask(id string) (*model.DeployTask, bool) {
	var t *model.DeployTask
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(deployTasksBucket)
		val := b.Get([]byte(id))
		if val == nil {
			return nil
		}
		t = &model.DeployTask{}
		return json.Unmarshal(val, t)
	}); err != nil {
		log.Printf("FSM GetDeployTask 读取失败: %v", err)
		return nil, false
	}
	if t == nil {
		return nil, false
	}
	return t, true
}

// --- SSH 凭据 ---

func (f *FSM) applyAddCredential(tx *bbolt.Tx, c model.SSHCredential) error {
	b := tx.Bucket(credentialsBucket)
	val, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return b.Put([]byte(c.ID), val)
}

func (f *FSM) applyDelCredential(tx *bbolt.Tx, id string) error {
	b := tx.Bucket(credentialsBucket)
	return b.Delete([]byte(id))
}

func (f *FSM) GetCredential(id string) (*model.SSHCredential, bool) {
	var c *model.SSHCredential
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(credentialsBucket)
		val := b.Get([]byte(id))
		if val == nil {
			return nil
		}
		c = &model.SSHCredential{}
		return json.Unmarshal(val, c)
	}); err != nil {
		log.Printf("FSM GetCredential 读取失败: %v", err)
		return nil, false
	}
	if c == nil {
		return nil, false
	}
	return c, true
}

func (f *FSM) ListCredentials() []model.SSHCredential {
	out := make([]model.SSHCredential, 0)
	if err := f.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(credentialsBucket)
		return b.ForEach(func(k, v []byte) error {
			var c model.SSHCredential
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			out = append(out, c)
			return nil
		})
	}); err != nil {
		log.Printf("FSM ListCredentials 读取失败: %v", err)
	}
	return out
}

// --- Snapshot / Restore ---

type snapshotData struct {
	Servers         map[string]model.Server
	Users           map[string]model.User
	Projects        map[string]model.ProjectRecord
	DeployTasks     map[string]model.DeployTask
	Credentials     map[string]model.SSHCredential
	ConfigVersions  map[string]model.ConfigVersion // v0.6.5 配置基准版本历史
}

// Snapshot 打包当前状态, 供 Raft 压缩日志和给新节点同步用。
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	data := snapshotData{
		Servers:        make(map[string]model.Server),
		Users:          make(map[string]model.User),
		Projects:       make(map[string]model.ProjectRecord),
		DeployTasks:    make(map[string]model.DeployTask),
		Credentials:    make(map[string]model.SSHCredential),
		ConfigVersions: make(map[string]model.ConfigVersion),
	}
	if err := f.db.View(func(tx *bbolt.Tx) error {
		readBucket := func(name []byte, fn func(k, v []byte) error) {
			if b := tx.Bucket(name); b != nil {
				if err := b.ForEach(fn); err != nil {
					log.Printf("[警告] FSM Snapshot 读取 bucket %s 出错: %v", string(name), err)
				}
			}
		}
		readBucket(serversBucket, func(k, v []byte) error {
			var s model.Server
			if err := json.Unmarshal(v, &s); err == nil {
				data.Servers[string(k)] = s
			}
			return nil
		})
		readBucket(usersBucket, func(k, v []byte) error {
			var u model.User
			if err := json.Unmarshal(v, &u); err == nil {
				data.Users[string(k)] = u
			}
			return nil
		})
		readBucket(projectsBucket, func(k, v []byte) error {
			var p model.ProjectRecord
			if err := json.Unmarshal(v, &p); err == nil {
				data.Projects[string(k)] = p
			}
			return nil
		})
		readBucket(deployTasksBucket, func(k, v []byte) error {
			var t model.DeployTask
			if err := json.Unmarshal(v, &t); err == nil {
				data.DeployTasks[string(k)] = t
			}
			return nil
		})
		readBucket(credentialsBucket, func(k, v []byte) error {
			var c model.SSHCredential
			if err := json.Unmarshal(v, &c); err == nil {
				data.Credentials[string(k)] = c
			}
			return nil
		})
		readBucket(configVersionsBucket, func(k, v []byte) error {
			var cv model.ConfigVersion
			if err := json.Unmarshal(v, &cv); err == nil {
				data.ConfigVersions[string(k)] = cv
			}
			return nil
		})
		return nil
	}); err != nil {
		log.Printf("FSM Snapshot 读取失败: %v", err)
	}
	return &fsmSnapshot{data: data}, nil
}

// Restore 在节点启动时从快照恢复。
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var data snapshotData
	if err := gob.NewDecoder(rc).Decode(&data); err != nil {
		return fmt.Errorf("恢复快照失败: %w", err)
	}
	if err := f.db.Update(func(tx *bbolt.Tx) error {
		// 逐个 bucket: 删旧重建
		for _, name := range [][]byte{serversBucket, usersBucket, projectsBucket, deployTasksBucket, credentialsBucket, configVersionsBucket} {
			if err := tx.DeleteBucket(name); err != nil && err != bbolt.ErrBucketNotFound {
				return err
			}
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		restoreBucket := func(name []byte, items interface{}) error {
			b := tx.Bucket(name)
			switch m := items.(type) {
			case map[string]model.Server:
				for id, s := range m {
					val, err := json.Marshal(s)
					if err != nil {
						return fmt.Errorf("序列化 Server %s 失败: %w", id, err)
					}
					if err := b.Put([]byte(id), val); err != nil {
						return err
					}
				}
			case map[string]model.User:
				for name, u := range m {
					val, err := json.Marshal(u)
					if err != nil {
						return fmt.Errorf("序列化 User %s 失败: %w", name, err)
					}
					if err := b.Put([]byte(name), val); err != nil {
						return err
					}
				}
			case map[string]model.ProjectRecord:
				for id, p := range m {
					val, err := json.Marshal(p)
					if err != nil {
						return fmt.Errorf("序列化 Project %s 失败: %w", id, err)
					}
					if err := b.Put([]byte(id), val); err != nil {
						return err
					}
				}
			case map[string]model.DeployTask:
				for id, t := range m {
					val, err := json.Marshal(t)
					if err != nil {
						return fmt.Errorf("序列化 DeployTask %s 失败: %w", id, err)
					}
					if err := b.Put([]byte(id), val); err != nil {
						return err
					}
				}
			case map[string]model.SSHCredential:
				for id, c := range m {
					val, err := json.Marshal(c)
					if err != nil {
						return fmt.Errorf("序列化 Credential %s 失败: %w", id, err)
					}
					if err := b.Put([]byte(id), val); err != nil {
						return err
					}
				}
			case map[string]model.ConfigVersion:
				for id, cv := range m {
					val, err := json.Marshal(cv)
					if err != nil {
						return fmt.Errorf("序列化 ConfigVersion %s 失败: %w", id, err)
					}
					if err := b.Put([]byte(id), val); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := restoreBucket(serversBucket, data.Servers); err != nil {
			return err
		}
		if err := restoreBucket(usersBucket, data.Users); err != nil {
			return err
		}
		if err := restoreBucket(projectsBucket, data.Projects); err != nil {
			return err
		}
		if err := restoreBucket(deployTasksBucket, data.DeployTasks); err != nil {
			return err
		}
		if err := restoreBucket(credentialsBucket, data.Credentials); err != nil {
			return err
		}
		if err := restoreBucket(configVersionsBucket, data.ConfigVersions); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("写入恢复数据: %w", err)
	}
	log.Printf("FSM 从快照恢复: %d 服务器, %d 用户, %d 项目, %d 部署任务, %d 凭据, %d 配置版本",
		len(data.Servers), len(data.Users), len(data.Projects), len(data.DeployTasks), len(data.Credentials), len(data.ConfigVersions))
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

// Release 释放快照资源。
func (s *fsmSnapshot) Release() {}
