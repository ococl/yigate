package proxy

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

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
	return subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
}

// HTTPAuthOK 检查 HTTP 代理 Basic 认证
func HTTPAuthOK(header http.Header, users map[string]string) bool {
	if !AuthRequired(users) {
		return true
	}
	ah := header.Get("Proxy-Authorization")
	user, pass, ok := parseBasicAuth(ah)
	if !ok {
		return false
	}
	return Authenticate(user, pass, users)
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
