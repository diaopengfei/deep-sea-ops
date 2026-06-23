package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"
)

// masterKey 是 AES-GCM 主密钥, 用于加密 SSH 凭据。
// 从环境变量 MASTER_KEY 读取(32 字节 base64 编码), 未设置则自动生成(仅开发环境)。
var (
	masterKey     []byte
	masterKeyOnce sync.Once
)

// getKey 获取主密钥(懒加载, 首次调用时从环境变量读取)。
// 开发环境未设置时自动生成随机密钥(重启后已加密数据无法解密, 仅限开发)。
func getKey() ([]byte, error) {
	var err error
	masterKeyOnce.Do(func() {
		keyB64 := os.Getenv("MASTER_KEY")
		if keyB64 != "" {
			key, derr := base64.StdEncoding.DecodeString(keyB64)
			if derr != nil || len(key) != 32 {
				err = fmt.Errorf("MASTER_KEY 必须是 32 字节 base64 编码")
				return
			}
			masterKey = key
			return
		}
		// 开发环境: 自动生成随机密钥(重启后丢失, 仅限开发)
		masterKey = make([]byte, 32)
		if _, rerr := rand.Read(masterKey); rerr != nil {
			err = rerr
			return
		}
		fmt.Println("[警告] MASTER_KEY 未设置, 使用随机密钥(仅限开发, 重启后已加密凭据无法解密)")
	})
	return masterKey, err
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
