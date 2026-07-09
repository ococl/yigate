package proxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

// socks5ReplyPacket 表示 SOCKS5 回复包。
type socks5ReplyPacket struct {
	rep byte
	raw []byte
}

// readSOCKS5Reply 从连接读取完整的 SOCKS5 回复。
func readSOCKS5Reply(r io.Reader) (*socks5ReplyPacket, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if header[0] != 0x05 {
		return nil, errors.New("invalid socks5 reply version")
	}

	var addrLen int
	switch header[3] {
	case 0x01:
		addrLen = 4
	case 0x03:
		size := make([]byte, 1)
		if _, err := r.Read(size); err != nil {
			return nil, err
		}
		addrLen = int(size[0])
		header = append(header, size...)
	case 0x04:
		addrLen = 16
	default:
		return nil, fmt.Errorf("unsupported reply address type %d", header[3])
	}
	addr := make([]byte, addrLen+2)
	if _, err := r.Read(addr); err != nil {
		return nil, err
	}
	return &socks5ReplyPacket{rep: header[1], raw: append(header, addr...)}, nil
}

// readSOCKS5UDPHeader 解析 SOCKS5 UDP 请求头，返回目标地址、分片号和负载起始偏移。
func readSOCKS5UDPHeader(buf []byte) (addr string, frag byte, dataOff int, ok bool) {
	if len(buf) < 10 {
		return "", 0, 0, false
	}
	if buf[0] != 0x00 || buf[1] != 0x00 {
		return "", 0, 0, false
	}
	frag = buf[2]
	atyp := buf[3]
	dataOff = 4

	var host string
	switch atyp {
	case 0x01:
		if len(buf) < dataOff+4+2 {
			return "", 0, 0, false
		}
		host = net.IP(buf[dataOff : dataOff+4]).String()
		dataOff += 4
	case 0x03:
		if len(buf) < dataOff+1 {
			return "", 0, 0, false
		}
		l := int(buf[dataOff])
		dataOff++
		if len(buf) < dataOff+l+2 {
			return "", 0, 0, false
		}
		host = string(buf[dataOff : dataOff+l])
		dataOff += l
	case 0x04:
		if len(buf) < dataOff+16+2 {
			return "", 0, 0, false
		}
		host = net.IP(buf[dataOff : dataOff+16]).String()
		dataOff += 16
	default:
		return "", 0, 0, false
	}
	if len(buf) < dataOff+2 {
		return "", 0, 0, false
	}
	port := int(buf[dataOff])<<8 | int(buf[dataOff+1])
	dataOff += 2
	return net.JoinHostPort(host, strconv.Itoa(port)), frag, dataOff, true
}

// buildSOCKS5UDPHeader 构造 SOCKS5 UDP 回复头。
func buildSOCKS5UDPHeader(target string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	var header []byte
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			header = []byte{0x00, 0x00, 0x00, 0x01}
			header = append(header, ip4...)
		} else {
			header = []byte{0x00, 0x00, 0x00, 0x04}
			header = append(header, ip.To16()...)
		}
	} else {
		header = []byte{0x00, 0x00, 0x00, 0x03, byte(len(host))}
		header = append(header, []byte(host)...)
	}
	header = append(header, byte(port>>8), byte(port))
	return header, nil
}

func parsePort(s string) (int, bool) {
	p, err := strconv.Atoi(s)
	if err != nil || p < 0 || p > 65535 {
		return 0, false
	}
	return p, true
}
