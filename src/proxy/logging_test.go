package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultLogConfig(t *testing.T) {
	cfg := DefaultLogConfig()
	if cfg.Level != "info" || cfg.Output != "stdout" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestNormalizeLogConfig(t *testing.T) {
	cfg := LogConfig{Level: "DEBUG", Format: "CONSOLE", Output: "STDERR"}
	NormalizeLogConfig(&cfg)
	if cfg.Level != "debug" || cfg.Format != "console" || cfg.Output != "stderr" {
		t.Fatalf("unexpected normalized config: %+v", cfg)
	}
}

func TestValidateLogConfig(t *testing.T) {
	if err := ValidateLogConfig(LogConfig{Output: "file", File: ""}); err == nil {
		t.Fatal("expected error for file output without path")
	}
	if err := ValidateLogConfig(LogConfig{Rotation: "weekly"}); err == nil {
		t.Fatal("expected error for unsupported rotation")
	}
	if err := ValidateLogConfig(LogConfig{Level: "verbose"}); err == nil {
		t.Fatal("expected error for invalid level")
	}
	if err := ValidateLogConfig(LogConfig{Format: "xml"}); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestInitLogger(t *testing.T) {
	if err := InitLogger(LogConfig{Level: "info", Output: "stdout"}); err != nil {
		t.Fatal(err)
	}
	if Logger() == nil {
		t.Fatal("expected logger")
	}
	SyncLogger()
}

func TestInitLoggerFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	if err := InitLogger(LogConfig{Level: "info", Output: "file", File: logPath, Rotation: "size"}); err != nil {
		t.Fatal(err)
	}
	Logger().Info("test message")
	SyncLogger()
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestInitLoggerSizeRotationBackupAndCompress(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "size.log")
	if err := InitLogger(LogConfig{
		Level:      "info",
		Output:     "file",
		File:       logPath,
		Rotation:   "size",
		MaxSize:    1,
		MaxBackups: 1,
		Compress:   true,
	}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = 'x'
	}
	for i := 0; i < 12; i++ {
		Logger().Info(string(payload))
	}
	SyncLogger()
	if err := InitLogger(LogConfig{Level: "info", Output: "stdout"}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	backupCount := 0
	for _, entry := range entries {
		name := entry.Name()
		if name == "size.log" {
			continue
		}
		if strings.HasPrefix(name, "size-") || strings.HasPrefix(name, "size.log.") {
			backupCount++
		}
	}
	if backupCount == 0 {
		t.Fatalf("expected size-rotated backup, entries=%v", entries)
	}
}

func TestInitLoggerDailyRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "daily.log")
	if err := InitLogger(LogConfig{Level: "info", Output: "file", File: logPath, Rotation: "daily", Compress: true}); err != nil {
		t.Fatal(err)
	}
	Logger().Info("daily test")
	SyncLogger()
}

func TestInitLoggerInvalid(t *testing.T) {
	if err := InitLogger(LogConfig{Level: "invalid"}); err == nil {
		t.Fatal("expected error for invalid level")
	}
	if err := InitLogger(LogConfig{Output: "unknown"}); err == nil {
		t.Fatal("expected error for invalid output")
	}
}

func TestInitLoggerConcurrentReload(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.log")
	secondPath := filepath.Join(dir, "second.log")
	if err := InitLogger(LogConfig{Level: "info", Output: "file", File: firstPath}); err != nil {
		t.Fatal(err)
	}
	oldLogger := Logger()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				oldLogger.Infow("concurrent old logger write", "index", j)
			}
		}()
	}
	if err := InitLogger(LogConfig{Level: "info", Output: "file", File: secondPath}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	Logger().Info("new logger write")
	SyncLogger()
	if info, err := os.Stat(firstPath); err != nil || info.Size() == 0 {
		t.Fatalf("old log was not written: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(secondPath); err != nil || info.Size() == 0 {
		t.Fatalf("new log was not written: info=%v err=%v", info, err)
	}
}

func TestDailyRotateWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate.log")
	w := newDailyRotateWriter(path, false, 1)
	defer w.Close()

	if _, err := w.Write([]byte("line1\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("line2\n"))
	w.Close()
	w.Close() // idempotent
}

func TestDailyRotateWriterRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate2.log")
	w := newDailyRotateWriter(path, false, 1)
	defer w.Close()

	w.Write([]byte("before\n"))
	w.mu.Lock()
	w.date = w.date.Add(-48 * time.Hour)
	w.mu.Unlock()
	w.Write([]byte("after\n"))

	if _, err := os.Stat(path + "." + w.date.Format("2006-01-02")); err != nil {
		// rotation may have used current date; just ensure backup exists
		entries, _ := os.ReadDir(dir)
		if len(entries) < 2 {
			t.Fatalf("expected rotated backup, got %d entries", len(entries))
		}
	}
}

func TestDailyRotateWriterCleanOldLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.log")
	oldFile := path + ".2020-01-01"
	os.WriteFile(oldFile, []byte("old"), 0644)
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(oldFile, oldTime, oldTime)
	w := newDailyRotateWriter(path, false, 1)
	w.cleanOldLogs()
	w.Close()
	if _, err := os.Stat(oldFile); err == nil {
		t.Fatal("expected old log cleaned")
	}
}

func TestDailyRotateWriterClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closed.log")
	w := newDailyRotateWriter(path, false, 1)
	w.Close()
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("expected error writing to closed writer")
	}
}

func TestCompressLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compress.log")
	os.WriteFile(path, []byte("hello"), 0644)
	if err := compressLog(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".gz"); err != nil {
		t.Fatal("expected gz file")
	}
	if err := compressLog("/nonexistent"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSameDay(t *testing.T) {
	now := time.Now()
	if !sameDay(now, now) {
		t.Fatal("expected same day")
	}
	if sameDay(now, now.Add(-24*time.Hour)) {
		t.Fatal("expected different day")
	}
}
