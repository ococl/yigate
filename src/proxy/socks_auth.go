package proxy

import (
	"bufio"
	"errors"
	"io"
	"net"
)

func socks5NegotiateAuth(client net.Conn, r *bufio.Reader, users map[string]string) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", err
	}
	if header[0] != 0x05 {
		return "", errors.New("not socks5")
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(r, methods); err != nil {
		return "", err
	}

	if AuthRequired(users) {
		if !contains(methods, 0x02) {
			_, _ = client.Write([]byte{0x05, 0xFF})
			return "", errors.New("no acceptable auth method")
		}
		if _, err := client.Write([]byte{0x05, 0x02}); err != nil {
			return "", err
		}
		return socks5ServerAuth(client, r, users)
	}

	if !contains(methods, 0x00) {
		_, _ = client.Write([]byte{0x05, 0xFF})
		return "", errors.New("no acceptable auth method")
	}
	_, err := client.Write([]byte{0x05, 0x00})
	return "", err
}

func socks5ServerAuth(client net.Conn, r *bufio.Reader, users map[string]string) (string, error) {
	ver := make([]byte, 1)
	if _, err := io.ReadFull(r, ver); err != nil || ver[0] != 0x01 {
		return "", errors.New("invalid auth version")
	}
	ulen := make([]byte, 1)
	if _, err := io.ReadFull(r, ulen); err != nil {
		return "", err
	}
	uname := make([]byte, ulen[0])
	if _, err := io.ReadFull(r, uname); err != nil {
		return "", err
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(r, plen); err != nil {
		return "", err
	}
	passwd := make([]byte, plen[0])
	if _, err := io.ReadFull(r, passwd); err != nil {
		return "", err
	}
	if !Authenticate(string(uname), string(passwd), users) {
		_, _ = client.Write([]byte{0x01, 0x01})
		return "", errors.New("socks5 auth failed")
	}
	_, err := client.Write([]byte{0x01, 0x00})
	return string(uname), err
}
