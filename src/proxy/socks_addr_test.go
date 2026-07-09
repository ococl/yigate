package proxy

import (
	"bytes"
	"net"
	"testing"
)

func TestSocks5AddressBytes(t *testing.T) {
	tests := []struct {
		name string
		host string
		want []byte
	}{
		{"ipv4", "127.0.0.1", []byte{0x01, 127, 0, 0, 1}},
		{"ipv6", "::1", []byte{0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		{"domain", "example.com", append([]byte{0x03, 11}, []byte("example.com")...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := socks5AddressBytes(tc.host)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadSOCKS5AddressValue(t *testing.T) {
	ipv4 := bytes.NewReader([]byte{127, 0, 0, 1})
	got, err := readSOCKS5AddressValue(ipv4, 0x01)
	if err != nil || got != "127.0.0.1" {
		t.Fatalf("got %q err %v", got, err)
	}

	domain := bytes.NewReader([]byte{11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'})
	got, err = readSOCKS5AddressValue(domain, 0x03)
	if err != nil || got != "example.com" {
		t.Fatalf("got %q err %v", got, err)
	}

	ipv6 := bytes.NewReader(net.ParseIP("::1"))
	got, err = readSOCKS5AddressValue(ipv6, 0x04)
	if err != nil || got != "::1" {
		t.Fatalf("got %q err %v", got, err)
	}

	_, err = readSOCKS5AddressValue(bytes.NewReader(nil), 0xFF)
	if err == nil {
		t.Fatal("expected error for unsupported atyp")
	}
}

func TestReadSOCKS5PortValue(t *testing.T) {
	got, err := readSOCKS5PortValue(bytes.NewReader([]byte{0x1f, 0x90}))
	if err != nil || got != 8080 {
		t.Fatalf("got %d err %v", got, err)
	}
}
