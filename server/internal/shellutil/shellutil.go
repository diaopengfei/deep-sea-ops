// Package shellutil 提供 shell 转义与路径安全校验的公共工具函数。
//
// sshclient 和 platform 包都需要对参数做 shell 单引号转义和路径安全校验,
// 抽取到本包避免重复实现。
package shellutil

import (
	"fmt"
	"strings"
)

// Quote 对字符串做 shell 单引号转义, 防止注入。
// 单引号内的内容不被 shell 解释, 只需处理单引号本身: ' → '\''
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// SafePath 检查路径是否安全(不含 shell 元字符的路径形式)。
// 允许: 字母/数字/._-/:
// 拒绝: ; & | ` $ ( ) < > \n 等
func SafePath(p string) error {
	for _, r := range p {
		switch r {
		case ';', '&', '|', '`', '$', '(', ')', '<', '>', '\n', '\r':
			return fmt.Errorf("路径包含不安全字符 %q: %s", r, p)
		}
	}
	return nil
}
