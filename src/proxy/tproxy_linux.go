//go:build linux

package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

// Linux 专用常量
const (
	soOriginalDst     = 80 // SO_ORIGINAL_DST (SOL_IP)
	ip6tSoOriginalDst = 80 // IP6T_SO_ORIGINAL_DST (SOL_IPV6)
	ipTransparent     = 19 // IP_TRANSPARENT (SOL_IP)
)

var errNoOriginalDst = fmt.Errorf("failed to get original destination")

// originalDst 通过 SO_ORIGINAL_DST / IP6T_SO_ORIGINAL_DST 获取 REDIRECT 模式下的原始目标地址。
func originalDst(conn net.Conn) (string, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return "", fmt.Errorf("not a TCP connection: %T", conn)
	}
	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return "", fmt.Errorf("get raw conn: %w", err)
	}

	var origDst string
	var getErr error
	ctrlErr := rawConn.Control(func(fd uintptr) {
		// 先尝试 IPv6，失败后回退到 IPv4
		origDst, getErr = getOriginalDstV6(int(fd))
		if getErr != nil {
			origDst, getErr = getOriginalDstV4(int(fd))
		}
	})
	if ctrlErr != nil {
		return "", fmt.Errorf("control raw conn: %w", ctrlErr)
	}
	if getErr != nil {
		return "", getErr
	}
	return origDst, nil
}

// getOriginalDstV4 通过 SO_ORIGINAL_DST 获取 IPv4 连接的原始目标。
func getOriginalDstV4(fd int) (string, error) {
	var buf [128]byte
	size := uint32(len(buf))
	_, _, errno := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(syscall.SOL_IP),
		uintptr(soOriginalDst),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
	)
	if errno != 0 {
		return "", fmt.Errorf("getsockopt SO_ORIGINAL_DST: %w", errno)
	}
	if size < 8 {
		return "", fmt.Errorf("SO_ORIGINAL_DST returned too few bytes: %d", size)
	}
	// sockaddr_in: family(2) + port(2) + addr(4) + zero(8)
	family := binary.LittleEndian.Uint16(buf[0:2])
	if family != syscall.AF_INET {
		return "", fmt.Errorf("SO_ORIGINAL_DST returned non-IPv4 family: %d", family)
	}
	port := binary.BigEndian.Uint16(buf[2:4])
	ip := net.IPv4(buf[4], buf[5], buf[6], buf[7])
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port)), nil
}

// getOriginalDstV6 通过 IP6T_SO_ORIGINAL_DST 获取 IPv6 连接的原始目标。
func getOriginalDstV6(fd int) (string, error) {
	var buf [128]byte
	size := uint32(len(buf))
	_, _, errno := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(syscall.SOL_IPV6),
		uintptr(ip6tSoOriginalDst),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
	)
	if errno != 0 {
		return "", fmt.Errorf("getsockopt IP6T_SO_ORIGINAL_DST: %w", errno)
	}
	if size < 24 {
		return "", fmt.Errorf("IP6T_SO_ORIGINAL_DST returned too few bytes: %d", size)
	}
	// sockaddr_in6: family(2) + port(2) + flowinfo(4) + addr(16) + scope_id(4)
	family := binary.LittleEndian.Uint16(buf[0:2])
	if family != syscall.AF_INET6 {
		return "", fmt.Errorf("IP6T_SO_ORIGINAL_DST returned non-IPv6 family: %d", family)
	}
	port := binary.BigEndian.Uint16(buf[2:4])
	ip := make(net.IP, 16)
	copy(ip, buf[8:24])
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port)), nil
}

// CreateTProxyListener 创建支持 IP_TRANSPARENT 的 TPROXY 模式监听器。
func CreateTProxyListener(addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				// 设置 IP_TRANSPARENT，允许接受发往非本地地址的连接
				if opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, ipTransparent, 1); opErr != nil {
					return
				}
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	return lc.Listen(context.Background(), "tcp", addr)
}

// IsTProxySupported 返回当前平台是否支持透明代理。
func IsTProxySupported() bool { return true }
