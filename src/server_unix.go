//go:build !windows

package main

import (
	"context"
	"net"
	"runtime"
	"syscall"
)

// reuseAddrListen 创建一个开启 SO_REUSEADDR（及 Linux 的 SO_REUSEPORT）的 TCP 监听器。
// 开启这两个选项后，旧监听器关闭（或处于 TIME_WAIT）时，新监听器可立即
// 绑定同一地址，避免热加载监听地址回退到原端口时 bind: address already in use。
// 注意：Windows 使用另一实现（server_windows.go），因为 SO_REUSEPORT 与
// SO_REUSEADDR 的语义不同，且 syscall 包在 Windows 上无对应常量。
func reuseAddrListen(network, addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				if opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); opErr != nil {
					return
				}
				// SO_REUSEPORT 仅在 Linux 上设置：让同地址可被多实例复用，进一步
				// 降低热加载切换窗口。标准库 syscall 未导出该常量，Linux 上其值为 15。
				if runtime.GOOS == "linux" {
					const soReusePort = 15
					opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
				}
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	return lc.Listen(context.Background(), network, addr)
}
