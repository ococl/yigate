package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInspectCertificatesDirectory 验证目录扫描只认证书文件、并跳过子目录。
func TestInspectCertificatesDirectory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeSelfSignedTestCert(t, dir, "ca", now.AddDate(10, 0, 0), true)
	writeSelfSignedTestCert(t, dir, "leaf", now.AddDate(0, 0, 30), false)

	// 非证书文件与子目录都不应被扫描。
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("listen: \":0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSelfSignedTestCert(t, filepath.Join(dir, "nested"), "ignored", now.AddDate(0, 0, 1), false)

	items, err := inspectCertificates(dir, now)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("inspected %d certificates, want 2: %+v", len(items), items)
	}
	// 目录内按文件名排序：ca 在 leaf 前。
	if items[0].Subject != "ca" || items[1].Subject != "leaf" {
		t.Fatalf("unexpected order: %+v", items)
	}
	if !items[0].IsCA || items[1].IsCA {
		t.Fatalf("CA flag not detected: %+v", items)
	}
	if items[0].DaysLeft < 3000 {
		t.Fatalf("10-year CA days_left = %d", items[0].DaysLeft)
	}
}

// TestRunCertCheckExitCode 过期或临期证书返回 1，正常证书返回 0。
func TestRunCertCheckExitCode(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	healthy := writeSelfSignedTestCert(t, dir, "healthy", now.AddDate(1, 0, 0), false)
	if code := runCertCheck(healthy); code != 0 {
		t.Fatalf("healthy certificate exit code = %d, want 0", code)
	}

	soon := writeSelfSignedTestCert(t, dir, "soon", now.AddDate(0, 0, 5), false)
	if code := runCertCheck(soon); code != 1 {
		t.Fatalf("expiring certificate exit code = %d, want 1", code)
	}

	expired := writeSelfSignedTestCert(t, dir, "expired", now.AddDate(0, 0, -1), false)
	if code := runCertCheck(expired); code != 1 {
		t.Fatalf("expired certificate exit code = %d, want 1", code)
	}

	if code := runCertCheck(filepath.Join(dir, "missing.crt")); code != 1 {
		t.Fatalf("missing file exit code = %d, want 1", code)
	}
}
