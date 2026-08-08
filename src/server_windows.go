//go:build windows

package main

import "net"

// reuseAddrListen 在 Windows 上直接使用标准 net.Listen。
// Windows 的 SO_REUSEADDR 语义与其他平台不同（默认即允许绑定 TIME_WAIT 地址），
// 且不支持 SO_REUSEPORT，故无需额外设置 socket 选项。
func reuseAddrListen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}
