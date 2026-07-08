package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

func socks5AddressBytes(host string) ([]byte, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return append([]byte{0x01}, ipv4...), nil
		}
		if ipv6 := ip.To16(); ipv6 != nil {
			return append([]byte{0x04}, ipv6...), nil
		}
	}
	if len(host) == 0 || len(host) > 255 {
		return nil, fmt.Errorf("invalid socks5 domain length %d", len(host))
	}
	addr := []byte{0x03, byte(len(host))}
	addr = append(addr, []byte(host)...)
	return addr, nil
}

func readSOCKS5AddressValue(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		addr := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return "", err
		}
		addr := make([]byte, int(size[0]))
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		return string(addr), nil
	case 0x04:
		addr := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil
	default:
		return "", fmt.Errorf("unsupported socks5 address type 0x%02x", atyp)
	}
}

func readSOCKS5PortValue(r io.Reader) (int, error) {
	var raw [2]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint16(raw[:])), nil
}
