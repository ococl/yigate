package proxy

import (
	"net"
	"strings"
	"time"
)

// DialTarget 直接连接到目标
func DialTarget(target string) (net.Conn, error) {
	return DialTargetTimeout(target, ProxyDialTimeout())
}

func DialTargetTimeout(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", target, timeout)
}

func namesToSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = struct{}{}
		}
	}
	return set
}
