//go:build !linux

package proxy

import (
	"fmt"
	"net"
)

var errNoOriginalDst = fmt.Errorf("transparent proxy is only supported on Linux")

// originalDst 在非 Linux 平台上返回错误。
func originalDst(conn net.Conn) (string, error) {
	return "", errNoOriginalDst
}

// CreateTProxyListener 在非 Linux 平台上返回错误。
func CreateTProxyListener(addr string) (net.Listener, error) {
	return nil, errNoOriginalDst
}

// IsTProxySupported 返回当前平台是否支持透明代理。
func IsTProxySupported() bool { return false }
