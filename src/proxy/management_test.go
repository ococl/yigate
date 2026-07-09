package proxy

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestManagementServerHealthAndMetrics(t *testing.T) {
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{
			Status:        "up",
			Listen:        ":4479",
			UpstreamCount: 2,
			RouterReady:   true,
			ManagerReady:  true,
		}
	}, false)

	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	addr := m.ln.Addr().String()
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("/health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("expected health body")
	}

	resp, err = http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("/metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("metrics status = %d", resp.StatusCode)
	}
}

func TestManagementServerHealthNotReady(t *testing.T) {
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: false, ManagerReady: true}
	}, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestManagementServerMethodNotAllowed(t *testing.T) {
	m := NewManagementServer(nil, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestManagementServerPprof(t *testing.T) {
	m := NewManagementServer(nil, true)
	if !m.PprofEnabled() {
		t.Fatal("expected pprof enabled")
	}
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()
	resp, err := http.Get("http://" + addr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("pprof: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("pprof status = %d", resp.StatusCode)
	}
}

func TestManagementServerStartEmptyAddr(t *testing.T) {
	m := NewManagementServer(nil, false)
	if err := m.Start(""); err != nil {
		t.Fatalf("Start with empty addr: %v", err)
	}
	if m.ln != nil {
		t.Fatal("expected no listener for empty addr")
	}
}

func TestManagementServerStatusAndReload(t *testing.T) {
	reloaded := false
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", Listen: ":4479", UpstreamCount: 1, RouterReady: true, ManagerReady: true}
	}, false)
	m.SetStatusSnapshotGetter(func() StatusSnapshot {
		return StatusSnapshot{Status: "up", Listen: ":4479", UpstreamCount: 1}
	})
	m.SetReload(func() error {
		reloaded = true
		return nil
	})
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	addr := m.ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/api/v1/status")
	if err != nil {
		t.Fatalf("/api/v1/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), ":4479") {
		t.Fatalf("expected listen in status body, got %s", body)
	}

	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/reload", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("reload status = %d", resp2.StatusCode)
	}
	if !reloaded {
		t.Fatal("expected reload callback called")
	}

	// GET on reload should be 405
	resp3, err := http.Get("http://" + addr + "/api/v1/reload")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 405 {
		t.Fatalf("reload GET status = %d, want 405", resp3.StatusCode)
	}

	// POST on status should be 405
	req4, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/status", nil)
	resp4, err := http.DefaultClient.Do(req4)
	if err != nil {
		t.Fatal(err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != 405 {
		t.Fatalf("status POST status = %d, want 405", resp4.StatusCode)
	}
}

func TestManagementServerReloadNotConfigured(t *testing.T) {
	m := NewManagementServer(nil, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/reload", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 501 {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}

func TestManagementServerAuthToken(t *testing.T) {
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, true, "secret-token")
	m.SetStatusSnapshotGetter(func() StatusSnapshot {
		return StatusSnapshot{Status: "up"}
	})
	m.SetReload(func() error { return nil })
	if m.AuthToken() != "secret-token" {
		t.Fatal("expected auth token set")
	}
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("unauthenticated health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("health without token = %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated status: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with token = %d, want 200", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/reload", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated reload: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reload with token = %d, want 200", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, "http://"+addr+"/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated pprof: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pprof with token = %d, want 200", resp.StatusCode)
	}
}

func TestManagementSetters(t *testing.T) {
	m := NewManagementServer(nil, false)
	m.SetPprofEnabled(true)
	if !m.PprofEnabled() {
		t.Fatal("expected pprof enabled after setter")
	}
	called := false
	m.SetStatusGetter(func() HealthStatus { called = true; return HealthStatus{Status: "up"} })
	if m.getStatus == nil {
		t.Fatal("expected status getter set")
	}
	_ = m.getStatus()
	if !called {
		t.Fatal("expected status getter called")
	}

	snapshotCalled := false
	m.SetStatusSnapshotGetter(func() StatusSnapshot { snapshotCalled = true; return StatusSnapshot{} })
	if m.getStatusSnapshot == nil {
		t.Fatal("expected status snapshot getter set")
	}
	_ = m.getStatusSnapshot()
	if !snapshotCalled {
		t.Fatal("expected status snapshot getter called")
	}

	reloaded := false
	m.SetReload(func() error { reloaded = true; return nil })
	if m.reload == nil {
		t.Fatal("expected reload set")
	}
	_ = m.reload()
	if !reloaded {
		t.Fatal("expected reload called")
	}
}
