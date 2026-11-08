package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

func socks5ReadRequest(r *bufio.Reader) (cmd byte, target string, rep byte, err error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, "", 0x01, err
	}
	if header[0] != 0x05 {
		return 0, "", 0x01, errors.New("invalid version")
	}
	cmd = header[1]
	if header[2] != 0x00 {
		return 0, "", 0x01, errors.New("invalid reserved byte")
	}

	var host string
	switch header[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(r, ip); err != nil {
			return 0, "", 0x01, err
		}
		host = net.IP(ip).String()
	case 0x03: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return 0, "", 0x01, err
		}
		domain := make([]byte, l[0])
		if _, err := io.ReadFull(r, domain); err != nil {
			return 0, "", 0x01, err
		}
		// 域名会被写入 HTTP CONNECT 请求行、日志与目标策略匹配结果，
		// 必须限制为安全的 ASCII 字符，避免 CRLF 注入上游请求。
		if !validSOCKS5Domain(domain) {
			return 0, "", 0x08, errors.New("invalid domain name")
		}
		host = string(domain)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(r, ip); err != nil {
			return 0, "", 0x01, err
		}
		host = net.IP(ip).String()
	default:
		return 0, "", 0x08, fmt.Errorf("unsupported address type %d", header[3])
	}

	port := make([]byte, 2)
	if _, err := io.ReadFull(r, port); err != nil {
		return 0, "", 0x01, err
	}
	p := int(port[0])<<8 | int(port[1])
	return cmd, net.JoinHostPort(host, strconv.Itoa(p)), 0x00, nil
}

// validSOCKS5Domain 只接受域名可用的 ASCII 字符，
// 拒绝 CRLF、空格等可注入上游 HTTP CONNECT 请求行的字节。
func validSOCKS5Domain(domain []byte) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	for _, c := range domain {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func contains(b []byte, v byte) bool {
	for _, x := range b {
		if x == v {
			return true
		}
	}
	return false
}
