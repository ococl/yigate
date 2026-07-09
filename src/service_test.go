package main

import (
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
