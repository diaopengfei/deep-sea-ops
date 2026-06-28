package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/deepsea-ops/server/internal/model"
)

// ============================================================================
// API Token 认证 (v0.7.0 API 开放与集成)
// ============================================================================

// TokenStore 是 auth 包对 store 包的抽象接口, 避免循环依赖。
// store.Store 实现了这个接口。
type TokenStore interface {
	ListTokens() []model.APIToken
	UpdTokenLastUsed(id string, lastUsedAt int64) error
}

// apiTokenPrefix 是 API Token 明文前缀, 便于识别和检索。
const apiTokenPrefix = "dst_"

// apiTokenRandomBytes 是 Token 随机部分的字节数(32 字节 = 256 位熵)。
const apiTokenRandomBytes = 32

// lastUsedThrottle 是 LastUsedAt 写入节流窗口。
// 窗口内的重复鉴权不重复走 Raft 写, 避免 CI/CD 高频轮询造成写放大。
const lastUsedThrottle = 60 * time.Second

// tokenLastUsedCache 记录每个 Token 上次写入 LastUsedAt 的时间(unix 毫秒), 用于节流。
// 多节点各自独立节流, LastUsedAt 取近似值可接受(仅前端展示用, 不参与鉴权判定)。
var tokenLastUsedCache sync.Map // map[tokenID]int64

// GenerateAPIToken 生成一个新的 API Token 明文和对应的 sha256 哈希。
// 返回的明文格式: "dst_<base64url(32 bytes)>", 只在创建时返回一次。
// 服务端只存 sha256(明文) 的 hex, 验证时对传入 token 计算 sha256 比对。
func GenerateAPIToken() (plaintext, hashHex, prefix string, err error) {
	buf := make([]byte, apiTokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("生成随机数失败: %w", err)
	}
	plaintext = apiTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	hashHex = HashAPIToken(plaintext)
	prefix = plaintext[:len(apiTokenPrefix)+8] // "dst_" + 前 8 字符, 用于前端识别
	return plaintext, hashHex, prefix, nil
}

// HashAPIToken 计算 API Token 明文的 sha256 哈希(hex 编码)。
func HashAPIToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// VerifyAPIToken 在 TokenStore 中查找匹配的 Token。
// 遍历所有 Token 计算 sha256 比对(因为存的是哈希, 无法按哈希反查)。
// Token 数量通常很少(几十个), 线性扫描可接受。
// 命中后异步更新 LastUsedAt(走 Raft), 但做 60s 节流避免高频 API 调用写放大。
// 返回 token 信息和是否命中。
func VerifyAPIToken(s TokenStore, plaintext string) (*model.APIToken, bool) {
	if s == nil || !strings.HasPrefix(plaintext, apiTokenPrefix) {
		return nil, false
	}
	hash := HashAPIToken(plaintext)
	tokens := s.ListTokens()
	for i := range tokens {
		t := tokens[i]
		if hmac.Equal([]byte(t.TokenHash), []byte(hash)) {
			// 检查过期
			now := time.Now()
			if t.ExpiresAt > 0 && t.ExpiresAt < now.UnixMilli() {
				return nil, false
			}
			// 异步更新 LastUsedAt(节流: 60s 内不重复写 Raft, 避免高频 API 写放大)
			go func(id string, nowMs int64) {
				if v, ok := tokenLastUsedCache.Load(id); ok {
					if nowMs-v.(int64) < lastUsedThrottle.Milliseconds() {
						return // 节流窗口内, 跳过
					}
				}
				tokenLastUsedCache.Store(id, nowMs)
				_ = s.UpdTokenLastUsed(id, nowMs)
			}(t.ID, now.UnixMilli())
			return &t, true
		}
	}
	return nil, false
}

// SetTokenStore 注入 Token 存储, 启用 API Token 认证。
// 不调用时只支持 JWT。
func (m *Middleware) SetTokenStore(s TokenStore) {
	m.tokens = s
}

// extractAndVerifyWithFallback 先尝试 JWT, 失败再尝试 API Token(v0.7.0)。
// 优先级: Authorization: Bearer <jwt>  →  Authorization: Bearer <api_token>  →  X-API-Token: <api_token>
func extractAndVerifyWithFallback(r *http.Request, m *Middleware) (*Claims, error) {
	// 先尝试 JWT(原有逻辑)
	if claims, err := extractAndVerify(r); err == nil {
		return claims, nil
	}
	// JWT 失败, 尝试 API Token
	if m.tokens == nil {
		return nil, fmt.Errorf("未授权: JWT 校验失败且未启用 API Token")
	}
	tok := extractAPITokenFromHeader(r)
	if tok == "" {
		return nil, fmt.Errorf("未授权: 缺少有效凭证")
	}
	t, ok := VerifyAPIToken(m.tokens, tok)
	if !ok {
		return nil, fmt.Errorf("未授权: API Token 无效或已过期")
	}
	// 用 Token 关联的创建者角色构造 Claims
	now := time.Now()
	return &Claims{
		Username: "token:" + t.Name,
		Role:     t.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(now),
			Subject:  "api-token:" + t.ID,
		},
	}, nil
}

// extractAPITokenFromHeader 从 Authorization Bearer 或 X-API-Token 头提取 token。
func extractAPITokenFromHeader(r *http.Request) string {
	// 1. Authorization: Bearer <token> — 与 JWT 共用头, 靠前缀 dst_ 区分
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if strings.HasPrefix(parts[1], apiTokenPrefix) {
				return parts[1]
			}
		}
	}
	// 2. X-API-Token: <token> — 专用头, 适合不方便改 Authorization 的场景
	if tok := r.Header.Get("X-API-Token"); tok != "" {
		return tok
	}
	return ""
}
