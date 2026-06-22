package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/deepsea-ops/server/internal/model"

	"github.com/deepsea-ops/server/internal/store"
)

// ============================================================================
// 配置
// ============================================================================

// jwtSecret 是 JWT 签名密钥。生产环境务必通过环境变量 JWT_SECRET 设置,
// 部署时随机生成一个长字符串。留空则用默认值(仅限开发, 启动会打警告)。
var jwtSecret = []byte(getEnvDefault("JWT_SECRET", "deepsea-dev-secret-change-me"))

// tokenTTL 是 access token 有效期。短时效降低泄露风险, 到期用 refresh token 换新。
const tokenTTL = 30 * time.Minute

// refreshTTL 是 refresh token 有效期。较长, 配合 access token 实现无感刷新。
const refreshTTL = 24 * time.Hour

// getEnvDefault 读环境变量, 空则返回默认值。
func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(jwtSecret)
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
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(jwtSecret)
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
		return jwtSecret, nil
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
type Middleware struct {
	whitelist map[string]bool // 不需要鉴权的路径
}

// NewMiddleware 创建鉴权中间件, whitelist 里的路径跳过校验。
func NewMiddleware(whitelist ...string) *Middleware {
	m := &Middleware{whitelist: make(map[string]bool)}
	for _, p := range whitelist {
		m.whitelist[p] = true
	}
	return m
}

// Wrap 包装一个 http.HandlerFunc, 强制鉴权。
// 用法: mux.HandleFunc("/api/servers", mw.Wrap(handleListServers))
func (m *Middleware) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.whitelist[r.URL.Path] {
			next(w, r)
			return
		}
		claims, err := extractAndVerify(r)
		if err != nil {
			http.Error(w, "未授权: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(WithUser(r.Context(), claims)))
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
	count    int
	lockUntil time.Time
}

const (
	maxLoginFails     = 5              // 连续失败 5 次后锁定
	lockDuration      = 15 * time.Minute // 锁定 15 分钟
	failWindow        = 30 * time.Minute // 失败计数窗口
)

// Allow 判断该用户名当前是否允许尝试登录(未被锁定)。
func (l *loginLimiter) Allow(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.failures[username]
	if !ok {
		return true
	}
	if time.Now().Before(r.lockUntil) {
		return false
	}
	// 超过失败窗口则重置
	if time.Since(r.lockUntil.Add(failWindow)) > 0 && r.count >= maxLoginFails {
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
	r.count++
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

// InitAdminPassword 从环境变量 ADMIN_PASSWORD 读初始管理员密码,
// 空则返回默认值并打警告(仅开发用)。
func InitAdminPassword() string {
	p := os.Getenv("ADMIN_PASSWORD")
	if p == "" {
		log.Println("警告: 未设置 ADMIN_PASSWORD, 使用默认密码 admin123 (仅限开发)")
		return "admin123"
	}
	return p
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

// RecordLoginFail 记录一次登录失败。
func (svc *Service) RecordLoginFail(username string) {
	svc.limiter.RecordFail(username)
}

// RecordLoginSuccess 记录登录成功, 清零失败计数。
func (svc *Service) RecordLoginSuccess(username string) {
	svc.limiter.RecordSuccess(username)
}