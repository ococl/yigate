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

func contains(b []byte, v byte) bool {
	for _, x := range b {
		if x == v {
			return true
		}
	}
	return false
}
