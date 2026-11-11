package proxy

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/emmansun/gmsm/sm3"
)

// secureTokenEqual 先计算定长摘要再做常量时间比较。
// 直接对原文使用 ConstantTimeCompare 会因长度不同提前返回，泄露令牌长度。
//
// 摘要算法使用国密 SM3：这里只用于定长化与常量时间比较，不承担存储或完整性职责，
// 换成 SM3 属于合规口径统一，不构成本身的安全增强。
func secureTokenEqual(provided, expected string) bool {
	a := sm3.Sum([]byte(provided))
	b := sm3.Sum([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// AuthRequired 是否配置了认证
func AuthRequired(users map[string]string) bool {
	return len(users) > 0
}

// Authenticate 校验用户名密码
func Authenticate(username, password string, users map[string]string) bool {
	if !AuthRequired(users) {
		return true
	}
	pass, ok := users[username]
	if !ok {
		return false
	}
	return secureTokenEqual(password, pass)
}

// HTTPAuthOK 检查 HTTP 代理 Basic 认证，返回认证通过的用户名。
func HTTPAuthOK(header http.Header, users map[string]string) (string, bool) {
	if !AuthRequired(users) {
		return "", true
	}
	ah := header.Get("Proxy-Authorization")
	user, pass, ok := parseBasicAuth(ah)
	if !ok {
		return "", false
	}
	if !Authenticate(user, pass, users) {
		return "", false
	}
	return user, true
}

func parseBasicAuth(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return
	}
	payload, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return
	}
	pair := strings.SplitN(string(payload), ":", 2)
	if len(pair) != 2 {
		return
	}
	return pair[0], pair[1], true
}
