package proxy

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogConfig 定义日志输出规则。
type LogConfig struct {
	Level      string `yaml:"level"`       // debug, info, warn, error, fatal, panic
	Format     string `yaml:"format"`      // json 或 console
	Output     string `yaml:"output"`      // stdout, stderr, file
	File       string `yaml:"file"`        // 文件路径，output=file 时必填
	Rotation   string `yaml:"rotation"`    // size 或 daily
	MaxSize    int    `yaml:"max_size"`    // 单文件大小上限（MB），rotation=size 时生效
	MaxAge     int    `yaml:"max_age"`     // 日志保留天数（>=1）
	MaxBackups int    `yaml:"max_backups"` // 最多保留多少个滚动文件（size 模式下生效）
	Compress   bool   `yaml:"compress"`    // 是否对历史日志进行 gzip 压缩
}

// DefaultLogConfig 返回默认日志配置。
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		Rotation:   "size",
		MaxSize:    100,
		MaxAge:     7,
		MaxBackups: 10,
		Compress:   true,
	}
}

// NormalizeLogConfig 填充日志默认值。
func NormalizeLogConfig(cfg *LogConfig) {
	if cfg == nil {
		return
	}
	defaults := DefaultLogConfig()
	if cfg.Level == "" {
		cfg.Level = defaults.Level
	}
	cfg.Level = strings.ToLower(strings.TrimSpace(cfg.Level))
	if cfg.Format == "" {
		cfg.Format = defaults.Format
	}
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	if cfg.Output == "" {
		cfg.Output = defaults.Output
	}
	cfg.Output = strings.ToLower(strings.TrimSpace(cfg.Output))
	if cfg.Rotation == "" {
		cfg.Rotation = defaults.Rotation
	}
	cfg.Rotation = strings.ToLower(strings.TrimSpace(cfg.Rotation))
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = defaults.MaxSize
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = defaults.MaxAge
	}
	if cfg.MaxBackups <= 0 {
		cfg.MaxBackups = defaults.MaxBackups
	}
}

var (
	loggerVal atomic.Value // stores *zap.SugaredLogger
	logMu     sync.Mutex
	logWriter io.Closer
)

func init() {
	loggerVal.Store(zap.NewNop().Sugar())
}

// ValidateLogConfig 校验日志配置项。
func ValidateLogConfig(cfg LogConfig) error {
	if cfg.Output == "file" && cfg.File == "" {
		return fmt.Errorf("file path is required when output is file")
	}
	if cfg.Rotation != "" && cfg.Rotation != "size" && cfg.Rotation != "daily" {
		return fmt.Errorf("unsupported rotation %q", cfg.Rotation)
	}
	if _, err := zapcore.ParseLevel(cfg.Level); err != nil {
		return fmt.Errorf("invalid level %q", cfg.Level)
	}
	if cfg.Format != "" && cfg.Format != "json" && cfg.Format != "console" {
		return fmt.Errorf("unsupported format %q", cfg.Format)
	}
	return nil
}

func Logger() *zap.SugaredLogger {
	if l := loggerVal.Load(); l != nil {
		return l.(*zap.SugaredLogger)
	}
	return zap.NewNop().Sugar()
}

// InitLogger 根据配置初始化/替换全局 logger。
// 新 logger 构建完成后原子地切换，旧 logger 会先 Sync 再关闭其 writer，避免并发写入到已关闭的 writer。
func InitLogger(cfg LogConfig) error {
	NormalizeLogConfig(&cfg)

	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	switch cfg.Format {
	case "console":
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	default:
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	ws, closer, err := buildWriteSyncer(cfg)
	if err != nil {
		return err
	}

	core := zapcore.NewCore(encoder, ws, level)
	newLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)).Sugar()

	logMu.Lock()
	oldLogger := Logger()
	oldWriter := logWriter
	loggerVal.Store(newLogger)
	logWriter = closer
	logMu.Unlock()

	if oldLogger != nil {
		_ = oldLogger.Sync()
	}
	if oldWriter != nil {
		_ = oldWriter.Close()
	}
	return nil
}

// SyncLogger 刷新当前 logger 的缓冲。
func SyncLogger() {
	if l := Logger(); l != nil {
		_ = l.Sync()
	}
}

func buildWriteSyncer(cfg LogConfig) (zapcore.WriteSyncer, io.Closer, error) {
	switch cfg.Output {
	case "stderr":
		return zapcore.AddSync(os.Stderr), nil, nil
	case "stdout", "":
		return zapcore.AddSync(os.Stdout), nil, nil
	case "file":
		if cfg.File == "" {
			return nil, nil, fmt.Errorf("log file path is required when output is file")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.File), 0o750); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}
		if cfg.Rotation == "daily" {
			w := newDailyRotateWriter(cfg.File, cfg.Compress, cfg.MaxAge)
			return zapcore.AddSync(w), w, nil
		}
		w := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.MaxSize,
			MaxAge:     cfg.MaxAge,
			MaxBackups: cfg.MaxBackups,
			Compress:   cfg.Compress,
			LocalTime:  true,
		}
		return zapcore.AddSync(w), w, nil
	default:
		return nil, nil, fmt.Errorf("unsupported log output %q", cfg.Output)
	}
}

// dailyRotateWriter 按自然日滚动日志文件，支持可选 gzip 压缩和历史清理。
type dailyRotateWriter struct {
	path     string
	compress bool
	maxAge   int

	mu   sync.Mutex
	file *os.File
	date time.Time
	done chan struct{}
}

func newDailyRotateWriter(path string, compress bool, maxAge int) *dailyRotateWriter {
	w := &dailyRotateWriter{
		path:     path,
		compress: compress,
		maxAge:   maxAge,
		done:     make(chan struct{}),
	}
	w.reopen()
	go w.cleanupLoop()
	return w
}

func (w *dailyRotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.done:
		return 0, fmt.Errorf("dailyRotateWriter closed")
	default:
	}

	now := time.Now()
	if !sameDay(now, w.date) {
		w.rotateLocked()
	}
	if w.file == nil {
		return 0, fmt.Errorf("log file not open: %s", w.path)
	}
	return w.file.Write(p)
}

func (w *dailyRotateWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Sync()
	}
	return nil
}

func (w *dailyRotateWriter) Close() error {
	w.mu.Lock()
	select {
	case <-w.done:
		w.mu.Unlock()
		return nil
	default:
		close(w.done)
	}
	file := w.file
	w.file = nil
	w.mu.Unlock()
	if file != nil {
		return file.Close()
	}
	return nil
}

func (w *dailyRotateWriter) reopen() {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return
	}
	w.file = f
	w.date = time.Now()
}

func (w *dailyRotateWriter) rotateLocked() {
	if w.file == nil {
		w.reopen()
		return
	}
	_ = w.file.Close()
	w.file = nil

	yesterday := w.date.Add(-24 * time.Hour)
	suffix := yesterday.Format("2006-01-02")
	oldPath := w.path + "." + suffix

	// 如果已存在，追加序号
	if _, err := os.Stat(oldPath); err == nil {
		for i := 1; i <= 100; i++ {
			candidate := fmt.Sprintf("%s.%s.%d", w.path, suffix, i)
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				oldPath = candidate
				break
			}
		}
	}
	_ = os.Rename(w.path, oldPath)
	if w.compress {
		go func(old string) {
			if err := compressLog(old); err != nil {
				Logger().Errorw("compress log failed", "path", old, "error", err)
			}
		}(oldPath)
	}
	w.reopen()
}

func (w *dailyRotateWriter) cleanupLoop() {
	if w.maxAge <= 0 {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.cleanOldLogs()
		}
	}
}

func (w *dailyRotateWriter) cleanOldLogs() {
	cutoff := time.Now().AddDate(0, 0, -w.maxAge)
	dir := filepath.Dir(w.path)
	base := filepath.Base(w.path)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == base {
			continue
		}
		if !strings.HasPrefix(name, base+".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func sameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func compressLog(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	gzPath := path + ".gz"
	dst, err := os.Create(gzPath)
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(dst)
	_, err = io.Copy(gw, src)
	_ = gw.Close()
	_ = dst.Close()
	if err == nil {
		return os.Remove(path)
	}
	_ = os.Remove(gzPath)
	return err
}
