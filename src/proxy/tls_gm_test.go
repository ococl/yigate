package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitee.com/Trisia/gotlcp/tlcp"
	"github.com/emmansun/gmsm/pkcs"
	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
)

// gmTestCert 是测试用 SM2 证书材料。
type gmTestCert struct {
	certPEM []byte
	keyPEM  []byte
	cert    *smx509.Certificate
	key     *sm2.PrivateKey
}

func newGMTestCA(t testing.TB) gmTestCert {
	t.Helper()
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &smx509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "yigate test GM CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              smx509.KeyUsageCertSign | smx509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := smx509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	cert, err := smx509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return gmTestCert{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  marshalSM2Key(t, key, nil),
		cert:    cert,
		key:     key,
	}
}

func issueGMTestCert(t testing.TB, ca gmTestCert, cn string, usage smx509.KeyUsage) gmTestCert {
	t.Helper()
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	tmpl := &smx509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     usage,
		ExtKeyUsage:  []smx509.ExtKeyUsage{smx509.ExtKeyUsageServerAuth, smx509.ExtKeyUsageClientAuth},
	}
	der, err := smx509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	cert, err := smx509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf certificate: %v", err)
	}
	return gmTestCert{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  marshalSM2Key(t, key, nil),
		cert:    cert,
		key:     key,
	}
}

// marshalSM2Key 输出 PKCS#8 私钥；password 非空时输出加密 PKCS#8（PBES2-SM4-SM3）。
func marshalSM2Key(t testing.TB, key *sm2.PrivateKey, password []byte) []byte {
	t.Helper()
	var (
		der []byte
		err error
	)
	if len(password) == 0 {
		der, err = smx509.MarshalPKCS8PrivateKey(key)
	} else {
		var encrypter pkcs.PBESEncrypter = pkcs.NewSMPBESEncrypter(16, 10000)
		der, err = pkcs8.MarshalPrivateKey(key, password, encrypter)
	}
	if err != nil {
		t.Fatalf("marshal SM2 private key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// writeGMTestPair 把证书与私钥写入临时目录，返回路径。
func writeGMTestPair(t testing.TB, dir, prefix string, c gmTestCert) (certFile, keyFile string) {
	t.Helper()
	certFile = filepath.Join(dir, prefix+".crt")
	keyFile = filepath.Join(dir, prefix+".key")
	if err := os.WriteFile(certFile, c.certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, c.keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// startGMTestServer 启动一个回显服务，TLCP 或标准 TLS 由 options 决定。
func startGMTestServer(t testing.TB, options ServerTLSOptions) (net.Listener, string) {
	t.Helper()
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln, err := NewServerListener(base, options)
	if err != nil {
		_ = base.Close()
		t.Fatalf("wrap listener: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln, ln.Addr().String()
}

func gmClientDialer() *net.Dialer {
	return &net.Dialer{Timeout: 3 * time.Second}
}

func TestNormalizeTLSProtocol(t *testing.T) {
	cases := map[string]string{
		"":       TLSProtocolAuto,
		" AUTO ": TLSProtocolAuto,
		"gm":     TLSProtocolGM,
		"GM":     TLSProtocolGM,
		"tls":    TLSProtocolTLS,
	}
	for input, want := range cases {
		if got := NormalizeTLSProtocol(input); got != want {
			t.Fatalf("NormalizeTLSProtocol(%q) = %q, want %q", input, got, want)
		}
	}
	if err := ValidateTLSProtocol("bogus"); err == nil {
		t.Fatal("expected unsupported protocol to be rejected")
	}
	if _, err := ResolveGMCipherSuites([]string{"SM4_GCM"}); err == nil {
		t.Fatal("expected unknown cipher suite to be rejected")
	}
	suites, err := ResolveGMCipherSuites(nil)
	if err != nil || len(suites) != len(defaultGMCipherSuites) {
		t.Fatalf("default suites mismatch: %v %v", suites, err)
	}
}

func TestParseSM2KeyPairWithPassword(t *testing.T) {
	ca := newGMTestCA(t)
	leaf := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageDigitalSignature)
	password := []byte("test-key-password")
	encryptedKey := marshalSM2Key(t, leaf.key, password)

	if _, err := ParseSM2KeyPair(leaf.certPEM, encryptedKey, ""); err == nil {
		t.Fatal("encrypted key must not parse without password")
	}
	pair, err := ParseSM2KeyPair(leaf.certPEM, encryptedKey, string(password))
	if err != nil {
		t.Fatalf("parse encrypted key: %v", err)
	}
	if len(pair.Certificate) == 0 || pair.PrivateKey == nil {
		t.Fatal("expected certificate and private key")
	}
}

func TestGMListenerHandshake(t *testing.T) {
	dir := t.TempDir()
	ca := newGMTestCA(t)
	signCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageDigitalSignature)
	encCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageKeyEncipherment)

	signCertFile, signKeyFile := writeGMTestPair(t, dir, "sign", signCert)
	encCertFile, encKeyFile := writeGMTestPair(t, dir, "enc", encCert)

	_, addr := startGMTestServer(t, ServerTLSOptions{
		Protocol:       TLSProtocolGM,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
		CipherSuites:   []string{"ECDHE_SM4_GCM_SM3", "ECC_SM4_GCM_SM3"},
	})

	pool := smx509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.certPEM) {
		t.Fatal("append CA failed")
	}
	conn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, &tlcp.Config{
		RootCAs:    pool,
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("tlcp dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping-gm")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 16)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "ping-gm" {
		t.Fatalf("echo failed: %v %q", err, buf[:n])
	}
	// 标准 TLS 客户端在强制国密端口上必须握手失败。
	if c, err := tls.DialWithDialer(gmClientDialer(), "tcp", addr, &tls.Config{InsecureSkipVerify: true}); err == nil {
		c.Close()
		t.Fatal("standard TLS client must not negotiate on a gm-only listener")
	}
}

func TestAutoListenerAcceptsBothProtocols(t *testing.T) {
	dir := t.TempDir()
	ca := newGMTestCA(t)
	signCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageDigitalSignature)
	encCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageKeyEncipherment)

	signCertFile, signKeyFile := writeGMTestPair(t, dir, "sign", signCert)
	encCertFile, encKeyFile := writeGMTestPair(t, dir, "enc", encCert)

	tlsCertFile, tlsKeyFile, _ := generateTestCerts(t)
	_, addr := startGMTestServer(t, ServerTLSOptions{
		Protocol:       TLSProtocolAuto,
		CertFile:       tlsCertFile,
		KeyFile:        tlsKeyFile,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
		CipherSuites:   []string{"ECC_SM4_GCM_SM3"},
	})

	standard, err := tls.DialWithDialer(gmClientDialer(), "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("standard TLS dial on auto listener: %v", err)
	}
	defer standard.Close()
	if _, err := standard.Write([]byte("tls")); err != nil {
		t.Fatalf("tls write: %v", err)
	}
	buf := make([]byte, 8)
	_ = standard.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, err := standard.Read(buf); err != nil || string(buf[:n]) != "tls" {
		t.Fatalf("tls echo failed: %v %q", err, buf[:n])
	}

	pool := smx509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.certPEM) {
		t.Fatal("append CA failed")
	}
	gm, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, &tlcp.Config{
		RootCAs:    pool,
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("tlcp dial on auto listener: %v", err)
	}
	defer gm.Close()
	if _, err := gm.Write([]byte("gm")); err != nil {
		t.Fatalf("gm write: %v", err)
	}
	_ = gm.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, err := gm.Read(buf); err != nil || string(buf[:n]) != "gm" {
		t.Fatalf("gm echo failed: %v %q", err, buf[:n])
	}
}

func TestNegotiatorAutoFallsBackAndRemembers(t *testing.T) {
	certFile, keyFile, _ := generateTestCerts(t)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load test key pair: %v", err)
	}
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(base, &tls.Config{Certificates: []tls.Certificate{cert}})
	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// tls.Listener 的握手是惰性的，必须读一次才能完成握手，
				// 否则客户端只会看到 EOF。
				buf := make([]byte, 1)
				_, _ = c.Read(buf)
			}(conn)
		}
	}()
	defer tlsLn.Close()

	neg := &tlsNegotiator{}
	opts := ClientTLSOptions{Protocol: TLSProtocolAuto, InsecureSkipVerify: true}
	for i := 0; i < 2; i++ {
		conn, err := neg.dial(tlsLn.Addr().String(), opts, gmClientDialer())
		if err != nil {
			t.Fatalf("auto dial #%d: %v", i, err)
		}
		_ = conn.Close()
	}
	if protocol, _ := neg.state(); protocol != TLSProtocolTLS {
		t.Fatalf("expected downgrade to tls, got %q", protocol)
	}

	// gm 模式不做降级：连标准 TLS 服务端必须失败。
	gmOnly := &tlsNegotiator{}
	if conn, err := gmOnly.dial(tlsLn.Addr().String(), ClientTLSOptions{Protocol: TLSProtocolGM, InsecureSkipVerify: true}, gmClientDialer()); err == nil {
		_ = conn.Close()
		t.Fatal("gm-only negotiation must not fall back to standard TLS")
	}
}

func TestNegotiatorAutoRemembersGM(t *testing.T) {
	dir := t.TempDir()
	ca := newGMTestCA(t)
	signCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageDigitalSignature)
	encCert := issueGMTestCert(t, ca, "localhost", smx509.KeyUsageKeyEncipherment)
	signCertFile, signKeyFile := writeGMTestPair(t, dir, "sign", signCert)
	encCertFile, encKeyFile := writeGMTestPair(t, dir, "enc", encCert)

	_, addr := startGMTestServer(t, ServerTLSOptions{
		Protocol:       TLSProtocolGM,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
		CipherSuites:   []string{"ECC_SM4_GCM_SM3"},
	})

	caFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caFile, ca.certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	neg := &tlsNegotiator{}
	opts := ClientTLSOptions{Protocol: TLSProtocolAuto, CAFile: caFile, ServerName: "localhost"}
	conn, err := neg.dial(addr, opts, gmClientDialer())
	if err != nil {
		t.Fatalf("auto dial gm server: %v", err)
	}
	_ = conn.Close()
	if protocol, _ := neg.state(); protocol != TLSProtocolGM {
		t.Fatalf("expected gm, got %q", protocol)
	}
}

// standardTestCA 是 RSA 测试 CA，用于构造标准 TLS 的 mTLS 客户端证书。
type standardTestCA struct {
	cert    *x509.Certificate
	key     *rsa.PrivateKey
	certPEM []byte
}

func newStandardTestCA(t testing.TB) standardTestCA {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "yigate test TLS CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create RSA CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse RSA CA certificate: %v", err)
	}
	return standardTestCA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// issue 签发一张叶子证书，返回 PEM 编码的证书与 PKCS#8 私钥。
func (ca standardTestCA) issue(t testing.TB, cn string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func writePEMPair(t testing.TB, dir, prefix string, certPEM, keyPEM []byte) (certFile, keyFile string) {
	t.Helper()
	certFile = filepath.Join(dir, prefix+".crt")
	keyFile = filepath.Join(dir, prefix+".key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

func mustGMClientPair(t testing.TB, c gmTestCert) tlcp.Certificate {
	t.Helper()
	pair, err := ParseSM2KeyPair(c.certPEM, c.keyPEM, "")
	if err != nil {
		t.Fatalf("parse SM2 client key pair: %v", err)
	}
	return pair
}

// TestGMListenerMTLS 验证 TLCP 端口的双向认证：
// 无客户端证书、以及客户端证书由其他 CA 签发都必须握手失败。
func TestGMListenerMTLS(t *testing.T) {
	dir := t.TempDir()
	ca := newGMTestCA(t)
	signCertFile, signKeyFile := writeGMTestPair(t, dir, "sign",
		issueGMTestCert(t, ca, "localhost", smx509.KeyUsageDigitalSignature))
	encCertFile, encKeyFile := writeGMTestPair(t, dir, "enc",
		issueGMTestCert(t, ca, "localhost", smx509.KeyUsageKeyEncipherment))

	caFile := filepath.Join(dir, "client-ca.crt")
	if err := os.WriteFile(caFile, ca.certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	_, addr := startGMTestServer(t, ServerTLSOptions{
		Protocol:       TLSProtocolGM,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
		ClientCAFile:   caFile,
	})

	pool := smx509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.certPEM) {
		t.Fatal("append GM CA failed")
	}
	base := &tlcp.Config{RootCAs: pool, ServerName: "localhost"}

	if conn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, base); err == nil {
		_ = conn.Close()
		t.Fatal("TLCP handshake without client certificate must fail")
	}

	// 客户端证书由另一套 CA 签发，服务端必须拒绝。
	otherCA := newGMTestCA(t)
	otherConfig := &tlcp.Config{
		RootCAs:    pool,
		ServerName: "localhost",
		Certificates: []tlcp.Certificate{
			mustGMClientPair(t, issueGMTestCert(t, otherCA, "client", smx509.KeyUsageDigitalSignature)),
			mustGMClientPair(t, issueGMTestCert(t, otherCA, "client", smx509.KeyUsageKeyEncipherment)),
		},
	}
	if conn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, otherConfig); err == nil {
		_ = conn.Close()
		t.Fatal("TLCP handshake with certificate from an unknown CA must fail")
	}

	// 正确 CA 签发的双证书可以完成握手。
	clientConfig := &tlcp.Config{
		RootCAs:    pool,
		ServerName: "localhost",
		Certificates: []tlcp.Certificate{
			mustGMClientPair(t, issueGMTestCert(t, ca, "client", smx509.KeyUsageDigitalSignature)),
			mustGMClientPair(t, issueGMTestCert(t, ca, "client", smx509.KeyUsageKeyEncipherment)),
		},
	}
	conn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, clientConfig)
	if err != nil {
		t.Fatalf("TLCP mTLS dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("mtls")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 8)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "mtls" {
		t.Fatalf("echo failed: %v %q", err, buf[:n])
	}
}

// TestAutoListenerMTLSWithMixedCA 覆盖 auto 端口同时服务两类 mTLS 客户端：
// client_ca_file 里同时放标准 TLS CA 与 SM2 CA，两边各取所需。
func TestAutoListenerMTLSWithMixedCA(t *testing.T) {
	dir := t.TempDir()
	stdCA := newStandardTestCA(t)
	gmCA := newGMTestCA(t)

	tlsCertPEM, tlsKeyPEM := stdCA.issue(t, "localhost", x509.ExtKeyUsageServerAuth)
	tlsCertFile, tlsKeyFile := writePEMPair(t, dir, "server", tlsCertPEM, tlsKeyPEM)
	signCertFile, signKeyFile := writeGMTestPair(t, dir, "gm-sign",
		issueGMTestCert(t, gmCA, "localhost", smx509.KeyUsageDigitalSignature))
	encCertFile, encKeyFile := writeGMTestPair(t, dir, "gm-enc",
		issueGMTestCert(t, gmCA, "localhost", smx509.KeyUsageKeyEncipherment))

	mixedCA := append(append([]byte(nil), stdCA.certPEM...), gmCA.certPEM...)
	caFile := filepath.Join(dir, "client-ca.pem")
	if err := os.WriteFile(caFile, mixedCA, 0o600); err != nil {
		t.Fatal(err)
	}

	_, addr := startGMTestServer(t, ServerTLSOptions{
		Protocol:       TLSProtocolAuto,
		CertFile:       tlsCertFile,
		KeyFile:        tlsKeyFile,
		GMSignCertFile: signCertFile,
		GMSignKeyFile:  signKeyFile,
		GMEncCertFile:  encCertFile,
		GMEncKeyFile:   encKeyFile,
		ClientCAFile:   caFile,
	})

	// 标准 TLS 客户端：RSA 证书由混合 CA 文件中的标准 CA 校验。
	clientCertPEM, clientKeyPEM := stdCA.issue(t, "client", x509.ExtKeyUsageClientAuth)
	rsaPair, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("load RSA client pair: %v", err)
	}
	stdPool := x509.NewCertPool()
	if !stdPool.AppendCertsFromPEM(stdCA.certPEM) {
		t.Fatal("append RSA CA failed")
	}
	stdConn, err := tls.DialWithDialer(gmClientDialer(), "tcp", addr, &tls.Config{
		RootCAs:      stdPool,
		ServerName:   "localhost",
		Certificates: []tls.Certificate{rsaPair},
	})
	if err != nil {
		t.Fatalf("standard TLS mTLS dial: %v", err)
	}
	_ = stdConn.Close()

	// TLCP 客户端：SM2 双证书由混合 CA 文件中的 SM2 CA 校验。
	gmPool := smx509.NewCertPool()
	if !gmPool.AppendCertsFromPEM(gmCA.certPEM) {
		t.Fatal("append SM2 CA failed")
	}
	gmConn, err := tlcp.DialWithDialer(gmClientDialer(), "tcp", addr, &tlcp.Config{
		RootCAs:    gmPool,
		ServerName: "localhost",
		Certificates: []tlcp.Certificate{
			mustGMClientPair(t, issueGMTestCert(t, gmCA, "client", smx509.KeyUsageDigitalSignature)),
			mustGMClientPair(t, issueGMTestCert(t, gmCA, "client", smx509.KeyUsageKeyEncipherment)),
		},
	})
	if err != nil {
		t.Fatalf("TLCP mTLS dial on auto listener: %v", err)
	}
	_ = gmConn.Close()
}
