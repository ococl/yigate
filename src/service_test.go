package main

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kardianos/service"
)

func TestProgramStartStop(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `listen: 127.0.0.1:0
log:
  level: info
`)
	p := &program{cfgPath: path}
	svc := &mockService{}
	if err := p.Start(svc); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if p.server == nil {
		t.Fatal("expected server created")
	}
	if err := p.Stop(svc); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestProgramStartInvalidConfig(t *testing.T) {
	p := &program{cfgPath: "/nonexistent/config.yml"}
	if err := p.Start(&mockService{}); err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestPlatformServiceOptions(t *testing.T) {
	opts := platformServiceOptions()
	if opts == nil {
		t.Fatal("expected non-nil platform options")
	}
	// 各平台至少应注入一个恢复/重启相关选项，保证崩溃后可自动恢复。
	switch runtime.GOOS {
	case "windows":
		if _, ok := opts["OnFailure"]; !ok {
			t.Error("windows option missing OnFailure")
		}
	case "darwin":
		if _, ok := opts["KeepAlive"]; !ok {
			t.Error("darwin option missing KeepAlive")
		}
	default:
		if opts["Restart"] != "always" {
			t.Errorf("linux/default Restart = %v, want always", opts["Restart"])
		}
		// SuccessExitStatus 已移除：退出码 1/2 和 SIGKILL 不应被视为成功退出
		if _, ok := opts["SuccessExitStatus"]; ok {
			t.Error("linux/default should not set SuccessExitStatus")
		}
		// RestartSec / StartLimitBurst 在自定义 systemd 模板中写死，
		// 需校验模板字符串而非 Option 键。
		tmpl, ok := opts["SystemdScript"].(string)
		if !ok || tmpl == "" {
			t.Fatal("linux/default missing SystemdScript template")
		}
		if !strings.Contains(tmpl, "RestartSec=10") {
			t.Error("SystemdScript template missing RestartSec=10")
		}
		if !strings.Contains(tmpl, "StartLimitBurst=64") {
			t.Error("SystemdScript template missing StartLimitBurst=64")
		}
		if !strings.Contains(tmpl, "{{.Restart}}") || !strings.Contains(tmpl, "{{.SuccessExitStatus}}") {
			t.Error("SystemdScript template should keep dynamic Restart/SuccessExitStatus")
		}
	}
}

func TestServiceStatusString(t *testing.T) {
	cases := map[service.Status]string{
		service.StatusRunning: "yigate service: running",
		service.StatusStopped: "yigate service: stopped",
		service.StatusUnknown: "yigate service: unknown (not installed or status unavailable)",
	}
	for st, want := range cases {
		if got := serviceStatusString(st); got != want {
			t.Errorf("serviceStatusString(%v) = %q, want %q", st, got, want)
		}
	}
	// 其余未定义状态走 default 分支，返回含 status= 文本。
	if got := serviceStatusString(service.Status(99)); got == "" {
		t.Error("expected non-empty string for unknown status code")
	}
}

type mockService struct{}

func (m *mockService) Run() error                                       { return nil }
func (m *mockService) Start() error                                     { return nil }
func (m *mockService) Stop() error                                      { return nil }
func (m *mockService) Restart() error                                   { return nil }
func (m *mockService) Install() error                                   { return nil }
func (m *mockService) Uninstall() error                                 { return nil }
func (m *mockService) Logger(errs chan<- error) (service.Logger, error) { return nil, nil }
func (m *mockService) SystemLogger(errs chan<- error) (service.Logger, error) {
	return nil, nil
}
func (m *mockService) String() string                  { return "mock" }
func (m *mockService) Platform() string                { return "mock-platform" }
func (m *mockService) Status() (service.Status, error) { return service.StatusUnknown, nil }
