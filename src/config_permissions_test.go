package main

import (
	"os"
	"testing"
)

// 运行配置可能含凭据与上游地址，任何写回都必须收紧为 0600。
func TestConfigEditorTightensFilePermissions(t *testing.T) {
	editor, cfgPath := newTestConfigEditor(t, `listen: "127.0.0.1:0"
management: "127.0.0.1:8890"
routing:
  final: direct
`)
	cfg, err := editor.loadRawConfig()
	if err != nil {
		t.Fatalf("loadRawConfig: %v", err)
	}
	if err := editor.saveAndApply(cfg); err != nil {
		t.Fatalf("saveAndApply: %v", err)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config file mode = %o, want 600", got)
	}
}
