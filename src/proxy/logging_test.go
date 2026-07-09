package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLogConfig(t *testing.T) {
	cfg := DefaultLogConfig()
	if cfg.Level != "info" || cfg.Format != "json" || cfg.Output != "stdout" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Rotation != "size" || cfg.MaxSize != 100 || cfg.MaxAge != 7 || cfg.MaxBackups != 10 || !cfg.Compress {
		t.Fatalf("unexpected rotation defaults: %+v", cfg)
	}
}

func TestNormalizeLogConfig(t *testing.T) {
	cfg := LogConfig{}
	NormalizeLogConfig(&cfg)
	if cfg.Level != "info" || cfg.Format != "json" || cfg.Output != "stdout" {
		t.Fatalf("normalization failed: %+v", cfg)
	}
}

func TestValidateLogConfig(t *testing.T) {
	if err := ValidateLogConfig(LogConfig{Level: "info", Output: "file"}); err == nil {
		t.Fatal("expected error for file output without path")
	}
	if err := ValidateLogConfig(LogConfig{Level: "info", Output: "file", File: "/tmp/x.log", Rotation: "daily"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateLogConfig(LogConfig{Level: "unknown"}); err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestInitLogger(t *testing.T) {
	dir := t.TempDir()
	cfg := LogConfig{
		Level:    "debug",
		Format:   "console",
		Output:   "file",
		File:     filepath.Join(dir, "test.log"),
		Rotation: "size",
		MaxSize:  1,
	}
	if err := InitLogger(cfg); err != nil {
		t.Fatalf("init logger: %v", err)
	}
	Logger().Infow("test message", "key", "value")
	SyncLogger()
	if _, err := os.Stat(cfg.File); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}
