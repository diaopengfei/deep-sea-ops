package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/deepsea-ops/server/internal/audit"
	"github.com/deepsea-ops/server/internal/model"

	"github.com/deepsea-ops/server/internal/store"
)

// ============================================================================
// 配置
// ============================================================================

// jwtSecret 是 JWT 签名密钥。由 main.go 在启动时通过 InitJWTSecret 显式注入,
// 来源优先级: 环境变量 JWT_SECRET > YAML 配置 security.jwt_secret > 内置默认值。
//
// 多节点 Raft 集群要求所有节点使用同一 JWT_SECRET, 否则入口代理转发请求到
// 非签发节点时鉴权失败。
var (
	jwtSecret     []byte
	jwtSecretOnce sync.Once
)

// InitJWTSecret 初始化 JWT 签名密钥。必须在 IssueTokens / ParseToken 之前调用。
// 重复调用会被忽略(以首次为准)。
func InitJWTSecret(secret string) {
	jwtSecretOnce.Do(func() {
		jwtSecret = []byte(secret)
	})
}

// getJWTSecret 获取已初始化的 JWT 密钥(未初始化时返回 nil)。
func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		// 未显式初始化, 用空 slice 触发后续签名失败, 避免静默使用默认值
		jwtSecret = []byte{}
	})
	return jwtSecret
}

// tokenTTL 是 access token 有效期。短时效降低泄露风险, 到期用 refresh token 换新。
const tokenTTL = 30 * time.Minute

// refreshTTL 是 refresh token 有效期。较长, 配合 access token 实现无感刷新。
const refreshTTL = 24 * time.Hour

// ============================================================================
// 密码哈希 (bcrypt)
// ============================================================================

// HashPassword 用 bcrypt 加盐哈希密码。cost=10 是默认值, 约 60ms/次, 兼顾安全与性能。
// 绝不存明文密码, 存的是哈希; 登录时用 ComparePassword 比对。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// ComparePassword 比对明文密码和已存储的哈希。返回 nil 表示匹配。
// bcrypt 内部从哈希串里解析出 salt 和 cost, 所以不需要单独存 salt。
func ComparePassword(hashed, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
}

// ============================================================================
// JWT 签发与校验
// ============================================================================

// Claims 是 JWT 载荷, 嵌入标准 RegisteredClaims 拿到 exp/iat/sub 等字段。
type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// TokenPair 是登录成功后返回给前端的令牌对。
// AccessToken 短时效, 放 Authorization 头; RefreshToken 长时效, 仅用于换新 access。
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"` // unix 时间戳, 前端据此判断过期
}

// IssueTokens 为已通过密码校验的用户签发令牌对。
func IssueTokens(u model.User) (*TokenPair, error) {
	now := time.Now()
	exp := now.Add(tokenTTL)

	// access token: 短时效, 携带用户名和角色
	accessClaims := &Claims{
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   u.Username,
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(getJWTSecret())
	if err != nil {
		return nil, fmt.Errorf("签发 access token: %w", err)
	}

	// refresh token: 长时效, 只带用户名(不带角色, 换新 access 时重新查库拿角色)
	refreshClaims := &Claims{
		Username: u.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(refreshTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   u.Username,
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(getJWTSecret())
	if err != nil {
		return nil, fmt.Errorf("签发 refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    exp.Unix(),
	}, nil
}

// ParseToken 解析并校验 JWT。签名错误或过期都返回 error。
func ParseToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		// 确保用的是 HMAC 签名方法, 防止 alg=none 攻击
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期签名方法: %v", t.Header["alg"])
		}
		return getJWTSecret(), nil
	})
	return claims, err
}

// ============================================================================
// 鉴权中间件
// ============================================================================

// contextKey 是自定义类型, 避免与其他包的 context key 冲突。
type contextKey string

const userKey contextKey = "user"

// WithUser 把 Claims 放入 context, 供后续 handler 读取当前用户。
func WithUser(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, userKey, claims)
}

// FromContext 从 context 取出当前用户 Claims, 无则返回 nil。
func FromContext(ctx context.Context) *Claims {
	v, _ := ctx.Value(userKey).(*Claims)
	return v
}

// Middleware 是 HTTP 鉴权中间件。
// 它从 Authorization 头提取 Bearer token, 校验 JWT, 通过则把 Claims 塞进 ctx。
// 未通过返回 401。白名单路径(如 /api/login)应绕过此中间件。
//
// v0.6.4: 若注入了 audit.Store, 写操作(POST/PUT/DELETE)会在处理完成后异步记录审计日志。
type Middleware struct {
	whitelist map[string]bool   // 不需要鉴权的路径
	audit     *audit.Store      // 可选, 审计日志存储
	tokens    TokenStore        // v0.7.0 可选, API Token 存储(启用后支持 API Token 鉴权)
}

// NewMiddleware 创建鉴权中间件, whitelist 里的路径跳过校验。
func NewMiddleware(whitelist ...string) *Middleware {
	m := &Middleware{whitelist: make(map[string]bool)}
	for _, p := range whitelist {
		m.whitelist[p] = true
	}
	return m
}

// SetAudit 注入审计日志存储。注入后, 受保护路由的写操作会自动记录审计。
func (m *Middleware) SetAudit(s *audit.Store) { m.audit = s }

// Wrap 包装一个 http.HandlerFunc, 强制鉴权。
// 用法: mux.HandleFunc("/api/servers", mw.Wrap(handleListServers))
//
// v0.7.0: 若注入了 TokenStore, JWT 校验失败时回退尝试 API Token 鉴权。
func (m *Middleware) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.whitelist[r.URL.Path] {
			next(w, r)
			return
		}
		claims, err := extractAndVerifyWithFallback(r, m)
		if err != nil {
			http.Error(w, "未授权: "+err.Error(), http.StatusUnauthorized)
			return
		}
		ctx := WithUser(r.Context(), claims)

		// v0.6.4: 写操作记录审计日志(异步, 不阻塞响应)
		if m.audit != nil && r.Method != http.MethodGet {
			rec := audit.NewStatusRecorder(w)
			next.ServeHTTP(rec, r.WithContext(ctx))
			action, target, sensitive := audit.Classify(r.Method, r.URL.Path)
			go m.audit.Record(audit.Log{
				Timestamp: time.Now().UnixMilli(),
				Username:  claims.Username,
				Role:      claims.Role,
				Method:    r.Method,
				Path:      r.URL.Path,
				Action:    action,
				Target:    target,
				Status:    rec.Status,
				IP:        audit.ClientIP(r),
				Sensitive: sensitive,
			})
			return
		}
		next(w, r.WithContext(ctx))
	}
}

// WrapWrite 包装 handler, 鉴权 + 要求写权限(v0.6.9)。
// 仅 admin / operator 可通过, viewer 返回 403。
func (m *Middleware) WrapWrite(next http.HandlerFunc) http.HandlerFunc {
	return m.requireRole(m.Wrap(next), model.RoleAdmin, model.RoleOperator)
}

// WrapAdmin 包装 handler, 鉴权 + 要求管理员权限(v0.6.9)。
// 仅 admin 可通过, 其他角色返回 403。用于用户管理等敏感操作。
func (m *Middleware) WrapAdmin(next http.HandlerFunc) http.HandlerFunc {
	return m.requireRole(m.Wrap(next), model.RoleAdmin)
}

// requireRole 在已有鉴权基础上追加角色检查。
// claims 由内层 Wrap 注入到 context; 不满足角色返回 403。
func (m *Middleware) requireRole(next http.HandlerFunc, roles ...string) http.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(w http.ResponseWriter, r *http.Request) {
		claims := FromContext(r.Context())
		if claims == nil || !allowed[claims.Role] {
			WriteJSON(w, http.StatusForbidden, map[string]string{"error": "权限不足"})
			return
		}
		next(w, r)
	}
}

// extractAndVerify 从请求头提取并校验 token。
func extractAndVerify(r *http.Request) (*Claims, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, fmt.Errorf("缺少 Authorization 头")
	}
	// 期望格式: "Bearer <token>"
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, fmt.Errorf("Authorization 格式错误")
	}
	return ParseToken(parts[1])
}

// ============================================================================
// 登录限流 (防暴力破解)
// ============================================================================

// loginLimiter 限制同一用户名的登录尝试频率。
// 连续失败 N 次后锁定一段时间, 防止暴力枚举密码。
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]*failRecord // key=username
}

type failRecord struct {
	count     int
	lockUntil time.Time
	lastFail  time.Time // 最近一次失败的时间, 用于判断失败窗口是否过期
}

const (
	maxLoginFails  = 5                // 连续失败 5 次后锁定
	lockDuration   = 15 * time.Minute // 锁定 15 分钟
	failWindow     = 30 * time.Minute // 失败计数窗口(超过此时间重置计数)
)

// Allow 判断该用户名当前是否允许尝试登录(未被锁定)。
func (l *loginLimiter) Allow(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.failures[username]
	if !ok {
		return true
	}
	// 锁定期内禁止登录
	if time.Now().Before(r.lockUntil) {
		return false
	}
	// 超过失败窗口(距上次失败超过 failWindow)则重置计数
	if !r.lastFail.IsZero() && time.Since(r.lastFail) > failWindow {
		delete(l.failures, username)
	}
	return true
}

// RecordFail 记录一次登录失败, 达到阈值则锁定。
func (l *loginLimiter) RecordFail(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.failures[username]
	if !ok {
		r = &failRecord{}
		l.failures[username] = r
	}
	// 超过失败窗口则重置计数(新一轮失败开始)
	if !r.lastFail.IsZero() && time.Since(r.lastFail) > failWindow {
		r.count = 0
	}
	r.count++
	r.lastFail = time.Now()
	if r.count >= maxLoginFails {
		r.lockUntil = time.Now().Add(lockDuration)
	}
}

// RecordSuccess 登录成功时清空该用户的失败记录。
func (l *loginLimiter) RecordSuccess(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, username)
}

// NewLoginLimiter 创建登录限流器。
func NewLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: make(map[string]*failRecord)}
}

// ============================================================================
// 登录响应
// ============================================================================

// LoginResponse 是 /api/login 成功时返回的 JSON 结构。
type LoginResponse struct {
	TokenPair
	Username string `json:"username"`
	Role     string `json:"role"`
}

// WriteJSON 是个小工具: 把 v 编码成 JSON 写到响应。
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ============================================================================
// AuthService: 整合 store, 提供登录/初始化管理员等业务方法
// ============================================================================

// Service 把鉴权所需的外部依赖(store)和工具函数组合起来, 供 api 层调用。
// 这样 api 层不需要直接操作 store 里的用户数据, 只通过 Service 访问。
type Service struct {
	store   *store.Store
	limiter *loginLimiter
}

// New 创建一个 AuthService。传入的 store 用于读写用户数据(用户走 Raft 保证多节点一致)。
func New(s *store.Store) *Service {
	return &Service{store: s, limiter: NewLoginLimiter()}
}

// CreateAdmin 首次启动时创建管理员账号。
// 若 admin 已存在则跳过; 否则用传入密码(bcrypt 哈希后)写入。



func (svc *Service) CreateAdmin(password string) error {
	// 查 admin 是否已存在
	if u, ok := svc.store.GetUser("admin"); ok && u != nil {
		return nil // 已存在, 不重复创建
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("哈希管理员密码失败: %w", err)
	}
	if err := svc.store.AddUser(model.User{
		Username:     "admin",
		PasswordHash: hash,
		Role:         "admin",
		CreatedAt:    time.Now().UnixMilli(),
	}); err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}
	log.Printf("已创建管理员账号 admin (role=admin)")
	return nil
}
// Login 处理登录请求: 校验密码, 成功则签发 token 对。
// 失败次数过多会被 loginLimiter 拦截(防爆破)。
func (svc *Service) Login(username, password string) (*LoginResponse, error) {
	if !svc.limiter.Allow(username) {
		return nil, fmt.Errorf("登录失败次数过多, 请稍后再试")
	}
	u, ok := svc.store.GetUser(username)
	if !ok || u == nil {
		svc.limiter.RecordFail(username)
		return nil, fmt.Errorf("用户名或密码错误")
	}
	if err := ComparePassword(u.PasswordHash, password); err != nil {
		svc.limiter.RecordFail(username)
		return nil, fmt.Errorf("用户名或密码错误")
	}
	svc.limiter.RecordSuccess(username)
	tp, err := IssueTokens(*u)
	if err != nil {
		return nil, fmt.Errorf("签发 token 失败: %w", err)
	}
	return &LoginResponse{TokenPair: *tp, Username: u.Username, Role: u.Role}, nil
}


// AllowLogin 判断该用户名是否允许登录(限流), 供 API 层调用, 封装 limiter 细节。
func (svc *Service) AllowLogin(username string) bool {
	return svc.limiter.Allow(username)
}