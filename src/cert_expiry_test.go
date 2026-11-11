package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"

	"yigate/proxy"
)

// writeSelfSignedTestCert 生成指定到期时间的自签证书，返回文件名。
func writeSelfSignedTestCert(t *testing.T, dir, name string, notAfter time.Time, isCA bool) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	path := filepath.Join(dir, name+".crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	return path
}

// writeSelfSignedGMTestCert 生成 SM2 自签证书，确认检查逻辑不依赖标准库解析器。
func writeSelfSignedGMTestCert(t *testing.T, dir, name string, notAfter time.Time) string {
	t.Helper()
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate SM2 key: %v", err)
	}
	tmpl := &smx509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              smx509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := smx509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create SM2 certificate: %v", err)
	}
	path := filepath.Join(dir, name+".crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	return path
}

// TestCollectExpiringCertificates 覆盖阈值判断、相对路径解析与过期标记。
func TestCollectExpiringCertificates(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 配置里写相对路径，检查必须按 baseDir 解析而不是工作目录。
	writeSelfSignedTestCert(t, filepath.Join(dir, "certs"), "soon", now.AddDate(0, 0, 10), true)
	writeSelfSignedTestCert(t, filepath.Join(dir, "certs"), "later", now.AddDate(1, 0, 0), true)
	expiredPath := writeSelfSignedTestCert(t, dir, "expired", now.AddDate(0, 0, -2), true)

	cfg := &Config{
		TLS: TLSConfig{CertFile: "certs/soon.crt"},
		Listeners: []ListenerConfig{
			{Listen: "127.0.0.1:0", TLS: TLSConfig{CertFile: "certs/later.crt"}},
		},
		Upstreams: []proxy.UpstreamConfig{{TLSRootCAFile: expiredPath}},
	}

	findings := collectExpiringCertificates(cfg, dir, now)
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2: %+v", len(findings), findings)
	}
	// 按到期时间升序：已过期的排在最前。
	if !findings[0].Expired {
		t.Fatalf("first finding should be the expired certificate: %+v", findings[0])
	}
	if findings[0].Kind != "upstream CA" {
		t.Fatalf("expired finding kind = %q, want upstream CA", findings[0].Kind)
	}
	if findings[1].Expired {
		t.Fatalf("10-day certificate must not be marked expired: %+v", findings[1])
	}
	if findings[1].Path != filepath.Join(dir, "certs", "soon.crt") {
		t.Fatalf("relative path not resolved against baseDir: %s", findings[1].Path)
	}
	if findings[1].Kind != "listener certificate" {
		t.Fatalf("soon finding kind = %q, want listener certificate", findings[1].Kind)
	}
}

// TestCollectExpiringCertificatesDedupesSharedFile 同一个 CA 文件被多个监听共用时只报一次。
func TestCollectExpiringCertificatesDedupesSharedFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := writeSelfSignedTestCert(t, dir, "shared", now.AddDate(0, 0, 5), true)

	cfg := &Config{
		Listeners: []ListenerConfig{
			{Listen: "127.0.0.1:1", TLS: TLSConfig{ClientCAFile: path}},
			{Listen: "127.0.0.1:2", TLS: TLSConfig{ClientCAFile: path}},
		},
	}
	if got := len(collectExpiringCertificates(cfg, dir, now)); got != 1 {
		t.Fatalf("findings = %d, want 1 (deduplicated by file)", got)
	}
}

// TestCollectExpiringCertificatesParsesSM2 国密证书必须能被解析，
// 标准库 x509 不认识 SM2，用错解析器会静默漏报。
func TestCollectExpiringCertificatesParsesSM2(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeSelfSignedGMTestCert(t, dir, "gm", now.AddDate(0, 0, 7))

	cfg := &Config{
		TLS: TLSConfig{GMSignCertFile: "gm.crt"},
	}
	findings := collectExpiringCertificates(cfg, dir, now)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 (SM2 certificate must be parsed)", len(findings))
	}
	if findings[0].Kind != "TLCP signing certificate" {
		t.Fatalf("kind = %q, want TLCP signing certificate", findings[0].Kind)
	}
	if findings[0].Expired {
		t.Fatal("certificate valid for 7 days must not be marked expired")
	}
}

// TestCollectExpiringCertificatesIgnoresMissingFile 文件读不到时静默跳过：
// 到期检查是附加能力，不能因为文件缺失让启动流程多出一条无意义告警。
func TestCollectExpiringCertificatesIgnoresMissingFile(t *testing.T) {
	cfg := &Config{TLS: TLSConfig{CertFile: "missing.crt"}}
	if got := collectExpiringCertificates(cfg, t.TempDir(), time.Now()); len(got) != 0 {
		t.Fatalf("findings = %d, want 0", len(got))
	}
}

// TestListCertificateFiles 覆盖 WebUI 证书路径补全的枚举规则：
// 只收证书扩展名、跳过隐藏目录、回传相对路径。
func TestListCertificateFiles(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"certs/gm", "certs/tls", ".git"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{
		"certs/gm/ca.crt",
		"certs/tls/ca.pem",
		"certs/tls/bundle.cer",
		"config.yml",      // 非证书扩展名
		".git/hidden.crt", // 隐藏目录不扫描
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := listCertificateFiles(dir)
	if err != nil {
		t.Fatalf("listCertificateFiles: %v", err)
	}
	want := []string{"certs/gm/ca.crt", "certs/tls/bundle.cer", "certs/tls/ca.pem"}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files = %v, want %v", got, want)
		}
	}
}

// TestListCertificateFilesMissingDir 目录不存在时返回错误，
// 让 API 能给出明确的 500 而不是空列表。
func TestListCertificateFilesMissingDir(t *testing.T) {
	if _, err := listCertificateFiles(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error for missing directory")
	}
}
