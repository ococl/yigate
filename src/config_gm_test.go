package main

import (
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/Trisia/gotlcp/tlcp"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
)

// writeGMTestMaterial 在临时目录写入占位证书文件，validation 只校验存在性。
func writeGMTestMaterial(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadConfigGMRequiresDoubleCertificate(t *testing.T) {
	dir := t.TempDir()
	writeGMTestMaterial(t, dir, "sign.crt", "sign.key")

	path := filepath.Join(dir, "config.yml")
	body := `listen: 127.0.0.1:0
tls:
  protocol: gm
  gm_sign_cert_file: sign.crt
  gm_sign_key_file: sign.key
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "gm_enc_cert_file") {
		t.Fatalf("expected missing encryption certificate error, got %v", err)
	}
}

func TestLoadConfigGMResolvesPathsAndPassword(t *testing.T) {
	dir := t.TempDir()
	writeGMTestMaterial(t, dir, "sign.crt", "sign.key", "enc.crt", "enc.key", "ca.crt")
	t.Setenv("YIGATE_GM_SIGN_PASSWORD", "sign-pass")
	t.Setenv("YIGATE_GM_ENC_PASSWORD", "enc-pass")

	path := filepath.Join(dir, "config.yml")
	body := `listen: 127.0.0.1:0
tls:
  protocol: gm
  gm_sign_cert_file: sign.crt
  gm_sign_key_file: sign.key
  gm_sign_key_password: "${YIGATE_GM_SIGN_PASSWORD}"
  gm_enc_cert_file: enc.crt
  gm_enc_key_file: enc.key
  gm_enc_key_password: "${YIGATE_GM_ENC_PASSWORD}"
  client_ca_file: ca.crt
  cipher_suites: ["ecdhE_sm4_GCM_sm3"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TLS.GMSignCertFile != filepath.Join(dir, "sign.crt") {
		t.Fatalf("sign cert path = %q", cfg.TLS.GMSignCertFile)
	}
	if cfg.TLS.GMEncKeyFile != filepath.Join(dir, "enc.key") {
		t.Fatalf("enc key path = %q", cfg.TLS.GMEncKeyFile)
	}
	if cfg.TLS.GMSignKeyPassword != "sign-pass" || cfg.TLS.GMEncKeyPassword != "enc-pass" {
		t.Fatalf("password not expanded: %q %q", cfg.TLS.GMSignKeyPassword, cfg.TLS.GMEncKeyPassword)
	}
	if cfg.TLS.NormalizedProtocol() != "gm" {
		t.Fatalf("protocol = %q", cfg.TLS.NormalizedProtocol())
	}
}

func TestLoadConfigAutoWithStandardCertificateOnly(t *testing.T) {
	dir := t.TempDir()
	writeGMTestMaterial(t, dir, "server.crt", "server.key")

	path := filepath.Join(dir, "config.yml")
	body := `listen: 127.0.0.1:0
tls:
  protocol: auto
  cert_file: server.crt
  key_file: server.key
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("auto protocol with standard certificate should load: %v", err)
	}
}

func TestLoadConfigRejectsInvalidProtocolAndCipherSuite(t *testing.T) {
	dir := t.TempDir()
	writeGMTestMaterial(t, dir, "server.crt", "server.key")

	for name, fragment := range map[string]string{
		"protocol": `  protocol: sslv3
  cert_file: server.crt
  key_file: server.key
`,
		"cipher": `  cert_file: server.crt
  key_file: server.key
  cipher_suites: ["SM4_GCM"]
`,
	} {
		path := filepath.Join(dir, "config-"+name+".yml")
		body := "listen: 127.0.0.1:0\ntls:\n" + fragment
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestLoadConfigUpstreamTLS(t *testing.T) {
	dir := t.TempDir()
	writeGMTestMaterial(t, dir, "gm-ca.crt")

	path := filepath.Join(dir, "config.yml")
	body := `listen: 127.0.0.1:0
upstreams:
  - name: "gm-gateway"
    url: "https://127.0.0.1:8443"
    tls_protocol: "gm"
    tls_ca_file: "gm-ca.crt"
    tls_server_name: "gw.example.com"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	up := cfg.Upstreams[0]
	if up.TLSProtocol != "gm" {
		t.Fatalf("tls_protocol = %q", up.TLSProtocol)
	}
	if up.TLSRootCAFile != filepath.Join(dir, "gm-ca.crt") {
		t.Fatalf("tls_ca_file not resolved: %q", up.TLSRootCAFile)
	}
}

func TestLoadConfigUpstreamGMRequiresHTTPS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	body := `listen: 127.0.0.1:0
upstreams:
  - name: "gm-socks"
    url: "socks5://127.0.0.1:1080"
    tls_protocol: "gm"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "requires an https upstream url") {
		t.Fatalf("expected https requirement error, got %v", err)
	}
}

// TestGMServerEndToEndFromGeneratedCerts 覆盖用户路径：
// gencert 生成证书 → 配置加载与校验 → 监听构造 → TLCP 客户端握手。
func TestGMServerEndToEndFromGeneratedCerts(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certs")
	if err := generateGMCertificates(certDir, "localhost", "", false); err != nil {
		t.Fatalf("gencert: %v", err)
	}

	path := filepath.Join(dir, "config.yml")
	body := fmt.Sprintf(`listen: 127.0.0.1:0
tls:
  protocol: gm
  gm_sign_cert_file: %q
  gm_sign_key_file: %q
  gm_enc_cert_file: %q
  gm_enc_key_file: %q
`, filepath.Join(certDir, "sign.crt"), filepath.Join(certDir, "sign.key"),
		filepath.Join(certDir, "enc.crt"), filepath.Join(certDir, "enc.key"))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ln, err := createListener("127.0.0.1:0", cfg.TLS, cfg.TProxy)
	if err != nil {
		t.Fatalf("createListener: %v", err)
	}
	defer ln.Close()

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

	caPEM, err := os.ReadFile(filepath.Join(certDir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	pool := smx509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("append CA failed")
	}
	conn, err := tlcp.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", ln.Addr().String(), &tlcp.Config{
		RootCAs:    pool,
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("tlcp dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello-gm")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 16)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "hello-gm" {
		t.Fatalf("echo mismatch: %v %q", err, buf[:n])
	}
}

// TestGMEncryptedKeyFromConfig 覆盖加密私钥 + 环境变量口令的完整路径。
func TestGMEncryptedKeyFromConfig(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certs")
	if err := generateGMCertificates(certDir, "localhost", "s3cret", false); err != nil {
		t.Fatalf("gencert: %v", err)
	}
	t.Setenv("YIGATE_GM_SIGN_PASSWORD", "s3cret")
	t.Setenv("YIGATE_GM_ENC_PASSWORD", "s3cret")

	path := filepath.Join(dir, "config.yml")
	body := fmt.Sprintf(`listen: 127.0.0.1:0
tls:
  protocol: gm
  gm_sign_cert_file: %q
  gm_sign_key_file: %q
  gm_sign_key_password: "${YIGATE_GM_SIGN_PASSWORD}"
  gm_enc_cert_file: %q
  gm_enc_key_file: %q
  gm_enc_key_password: "${YIGATE_GM_ENC_PASSWORD}"
`, filepath.Join(certDir, "sign.crt"), filepath.Join(certDir, "sign.key"),
		filepath.Join(certDir, "enc.crt"), filepath.Join(certDir, "enc.key"))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ln, err := createListener("127.0.0.1:0", cfg.TLS, cfg.TProxy)
	if err != nil {
		t.Fatalf("createListener with encrypted keys: %v", err)
	}
	_ = ln.Close()
}

// TestGenCertReuseExistingCA 覆盖 gencert -ca：复用既有 CA 签发叶子证书。
// 要求：签发结果能链到该 CA、不覆盖原 CA 文件、错误口令与参数缺失都明确失败。
func TestGenCertReuseExistingCA(t *testing.T) {
	base := t.TempDir()
	caDir := filepath.Join(base, "ca")
	if err := generateGMCertificates(caDir, "localhost", "ca-secret", false); err != nil {
		t.Fatalf("generate CA: %v", err)
	}
	caCertPath := filepath.Join(caDir, "ca.crt")
	caKeyPath := filepath.Join(caDir, "ca.key")
	caCertBefore, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatal(err)
	}

	leafDir := filepath.Join(base, "leaf")
	err = generateGMCertificatesWithOptions(genCertOptions{
		OutDir:        leafDir,
		Domain:        "gw.example.com",
		KeyPassword:   "leaf-secret",
		CACertFile:    caCertPath,
		CAKeyFile:     caKeyPath,
		CAKeyPassword: "ca-secret",
	})
	if err != nil {
		t.Fatalf("issue from existing CA: %v", err)
	}

	// 原 CA 不能被覆盖，否则信任链会在下次轮换时断裂。
	caCertAfter, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(caCertBefore) != string(caCertAfter) {
		t.Fatal("existing CA certificate was overwritten")
	}
	// 复用 CA 时不应产出新的 ca.key/ca.crt。
	if _, err := os.Stat(filepath.Join(leafDir, "ca.crt")); err == nil {
		t.Fatal("leaf dir must not contain a second CA certificate")
	}

	// 叶子证书必须由该 CA 签发，并且域名可用。
	pool := smx509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertAfter) {
		t.Fatal("append CA failed")
	}
	leafPEM, err := os.ReadFile(filepath.Join(leafDir, "sign.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(leafPEM)
	if block == nil {
		t.Fatal("decode leaf certificate failed")
	}
	leaf, err := smx509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if _, err := leaf.Verify(smx509.VerifyOptions{
		Roots:     pool,
		DNSName:   "gw.example.com",
		KeyUsages: []smx509.ExtKeyUsage{smx509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("leaf does not chain to the provided CA: %v", err)
	}

	// 加密的 CA 私钥：错误口令必须失败，不能静默产出无法使用的证书。
	badDir := filepath.Join(base, "bad")
	err = generateGMCertificatesWithOptions(genCertOptions{
		OutDir:        badDir,
		Domain:        "gw.example.com",
		CACertFile:    caCertPath,
		CAKeyFile:     caKeyPath,
		CAKeyPassword: "wrong",
	})
	if err == nil {
		t.Fatal("expected failure with wrong CA key password")
	}
	// 只给 -ca 不给 -ca-key 也要拒绝。
	if err := generateGMCertificatesWithOptions(genCertOptions{
		OutDir:     filepath.Join(base, "half"),
		Domain:     "gw.example.com",
		CACertFile: caCertPath,
	}); err == nil {
		t.Fatal("expected failure when -ca-key is missing")
	}
}

// readGeneratedCert 读取 gencert 产出的证书，便于断言有效期。
func readGeneratedCert(t *testing.T, dir, name string) *smx509.Certificate {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("%s does not contain a PEM certificate", name)
	}
	cert, err := smx509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return cert
}

// writeExpiredGMCA 伪造一张已过期的 SM2 CA，用于验证 gencert 拒绝签发。
func writeExpiredGMCA(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate expired CA key: %v", err)
	}
	tmpl := &smx509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "expired GM CA"},
		NotBefore:             time.Now().AddDate(-2, 0, 0),
		NotAfter:              time.Now().AddDate(-1, 0, 0),
		KeyUsage:              smx509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := smx509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create expired CA: %v", err)
	}
	keyPEM, err := marshalGMPrivateKey(key, nil)
	if err != nil {
		t.Fatalf("marshal expired CA key: %v", err)
	}
	certFile = filepath.Join(dir, "expired-ca.crt")
	keyFile = filepath.Join(dir, "expired-ca.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// TestGenCertValidityWindow 锁定 -days 与"叶子不得比 CA 活得久"的约束。
// 后者不满足时会签出看似有效、CA 一过期就整链失效的证书。
func TestGenCertValidityWindow(t *testing.T) {
	base := t.TempDir()

	// 1. -days 生效：1 天有效期的叶子证书。
	shortDir := filepath.Join(base, "short")
	if err := generateGMCertificatesWithOptions(genCertOptions{
		OutDir: shortDir,
		Domain: "localhost",
		Days:   1,
	}); err != nil {
		t.Fatalf("generate short-lived cert: %v", err)
	}
	shortLeaf := readGeneratedCert(t, shortDir, "sign.crt")
	if delta := time.Until(shortLeaf.NotAfter); delta > 48*time.Hour || delta < 0 {
		t.Fatalf("signing certificate valid for %v, want about 1 day", delta)
	}

	// 2. 复用 CA 时按 CA 的到期时间收敛。
	caDir := filepath.Join(base, "ca")
	if err := generateGMCertificates(caDir, "localhost", "", false); err != nil {
		t.Fatalf("generate CA: %v", err)
	}
	caCert := readGeneratedCert(t, caDir, "ca.crt")

	longDir := filepath.Join(base, "long")
	if err := generateGMCertificatesWithOptions(genCertOptions{
		OutDir:     longDir,
		Domain:     "localhost",
		Days:       36500,
		CACertFile: filepath.Join(caDir, "ca.crt"),
		CAKeyFile:  filepath.Join(caDir, "ca.key"),
	}); err != nil {
		t.Fatalf("issue long-lived cert: %v", err)
	}
	longLeaf := readGeneratedCert(t, longDir, "sign.crt")
	if longLeaf.NotAfter.After(caCert.NotAfter) {
		t.Fatalf("leaf expires %s, after CA %s", longLeaf.NotAfter, caCert.NotAfter)
	}
	if !longLeaf.NotAfter.Equal(caCert.NotAfter) {
		t.Fatalf("leaf NotAfter = %s, want capped to CA NotAfter %s", longLeaf.NotAfter, caCert.NotAfter)
	}

	// 3. 过期 CA 必须拒绝，而不是签出当场失效的证书。
	expiredCert, expiredKey := writeExpiredGMCA(t, base)
	err := generateGMCertificatesWithOptions(genCertOptions{
		OutDir:     filepath.Join(base, "expired"),
		Domain:     "localhost",
		CACertFile: expiredCert,
		CAKeyFile:  expiredKey,
	})
	if err == nil {
		t.Fatal("expected failure when the CA is already expired")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("unexpected error: %v", err)
	}
}
