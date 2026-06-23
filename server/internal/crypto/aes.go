// Package crypto 提供 SSH 凭据的 AES-GCM 加解密。
//
// 主密钥由调用方(main.go)在启动时通过 InitMasterKey 显式注入,
// 来源优先级: 环境变量 MASTER_KEY > YAML 配置 security.master_key > 开发模式随机生成。
//
// 多节点 Raft 集群要求所有节点使用同一 MASTER_KEY, 否则 Follower 当选 Leader 后
// 无法解密 Raft 中已加密的 SSH 凭据。生产环境务必显式设置。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	"github.com/deepsea-ops/server/internal/config"
)

var (
	masterKey     []byte
	masterKeyOnce sync.Once
	masterKeyErr  error
)

// InitMasterKey 初始化主密钥。
//   - keyB64 为 32 字节 base64 编码的密钥, 非空时直接使用
//   - keyB64 为空时进入开发模式: 随机生成一个密钥(重启后已加密数据无法解密, 仅限开发)
//
// 必须在 Encrypt/Decrypt 之前调用。重复调用会被忽略(以首次为准)。
func InitMasterKey(keyB64 string) {
	masterKeyOnce.Do(func() {
		if keyB64 != "" {
			key, err := base64.StdEncoding.DecodeString(keyB64)
			if err != nil || len(key) != 32 {
				masterKeyErr = fmt.Errorf("MASTER_KEY 必须是 32 字节 base64 编码")
				return
			}
			masterKey = key
			return
		}
		// 开发模式: 自动生成随机密钥(重启后丢失, 仅限开发)
		masterKey = make([]byte, 32)
		if _, err := rand.Read(masterKey); err != nil {
			masterKeyErr = err
			return
		}
		fmt.Println("[警告] MASTER_KEY 未设置, 使用随机密钥(仅限开发, 重启后已加密凭据无法解密)")
	})
}

// InitFromSecurityConfig 从 config.SecurityConfig 初始化主密钥。
// 便于 main.go 直接传入配置。
func InitFromSecurityConfig(sec config.SecurityConfig) {
	InitMasterKey(sec.MasterKey)
}

// getKey 获取已初始化的主密钥。
func getKey() ([]byte, error) {
	// 若未显式初始化, 触发一次默认初始化(开发模式随机密钥)
	masterKeyOnce.Do(func() {
		masterKey = make([]byte, 32)
		if _, err := rand.Read(masterKey); err != nil {
			masterKeyErr = err
			return
		}
		fmt.Println("[警告] MASTER_KEY 未设置, 使用随机密钥(仅限开发, 重启后已加密凭据无法解密)")
	})
	if masterKeyErr != nil {
		return nil, masterKeyErr
	}
	if masterKey == nil {
		return nil, errors.New("主密钥未初始化")
	}
	return masterKey, nil
}

// Encrypt 用 AES-GCM 加密明文, 返回 base64 编码的密文(含 nonce 前缀)。
func Encrypt(plaintext string) (string, error) {
	key, err := getKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	// nonce 拼在密文前面, 解密时取前 NonceSize 字节
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密 AES-GCM 密文(base64 编码, 含 nonce 前缀)。
func Decrypt(ciphertextB64 string) (string, error) {
	if ciphertextB64 == "" {
		return "", nil
	}
	key, err := getKey()
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("base64 解码失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", errors.New("密文太短")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	ct := ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}
	return string(plaintext), nil
}

// GenerateMasterKey 生成一个新的 32 字节主密钥(base64 编码), 供部署时生成用。
func GenerateMasterKey() string {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return base64.StdEncoding.EncodeToString(key)
}
