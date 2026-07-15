package proxy

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestManagementServerStartListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, false)
	if err := m.StartListener(ln); err != nil {
		t.Fatalf("StartListener: %v", err)
	}
	defer m.Stop()
	resp, err := http.Get("http://" + ln.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

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

func TestManagementServerPprofDisabled(t *testing.T) {
	m := NewManagementServer(nil, false)
	if m.PprofEnabled() {
		t.Fatal("expected pprof disabled")
	}
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()
	resp, err := http.Get("http://" + addr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("pprof disabled request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected disabled pprof endpoint to return non-200")
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

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("wrong token health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("health with wrong token = %d, want 401", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/status", nil)
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

// ============================================================
// /api/v1/traffic 端点测试
// ============================================================

func TestManagementTrafficEndpoint(t *testing.T) {
	// 设置全局流量统计器
	ta := NewTrafficAccounter(TrafficAuditConfig{Enabled: true})
	SetGlobalTrafficAccounter(ta)
	defer SetGlobalTrafficAccounter(nil)

	// 记录一些流量
	ta.Record(TrafficKey{User: "alice", Upstream: "up1", Target: "example.com"}, 100, 200)
	ta.Record(TrafficKey{User: "bob", Upstream: "up2", Target: "test.com"}, 50, 150)

	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/api/v1/traffic")
	if err != nil {
		t.Fatalf("traffic GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("traffic status = %d, want 200", resp.StatusCode)
	}
	var snap TrafficSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode traffic: %v", err)
	}
	if len(snap.Entries) != 2 {
		t.Fatalf("expected 2 traffic entries, got %d", len(snap.Entries))
	}
	// 验证条目内容
	found := false
	for _, e := range snap.Entries {
		if e.User == "alice" && e.Upstream == "up1" && e.Target == "example.com" {
			if e.BytesIn != 100 || e.BytesOut != 200 || e.Total != 300 {
				t.Fatalf("alice entry = %+v", e)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected alice traffic entry not found")
	}
}

func TestManagementTrafficMethodNotAllowed(t *testing.T) {
	m := NewManagementServer(nil, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/traffic", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("traffic POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("traffic POST status = %d, want 405", resp.StatusCode)
	}
}

func TestManagementTrafficWithAuth(t *testing.T) {
	m := NewManagementServer(nil, false, "traffic-token")
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	// 无认证应 401
	resp, err := http.Get("http://" + addr + "/api/v1/traffic")
	if err != nil {
		t.Fatalf("traffic no auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("traffic no auth status = %d, want 401", resp.StatusCode)
	}

	// 有认证应 200
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/api/v1/traffic", nil)
	req.Header.Set("Authorization", "Bearer traffic-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("traffic with auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("traffic with auth status = %d, want 200", resp.StatusCode)
	}
}

func TestManagementTrafficEmptyAccounter(t *testing.T) {
	// 确保无全局流量统计器时返回空快照
	SetGlobalTrafficAccounter(nil)
	defer SetGlobalTrafficAccounter(nil)

	m := NewManagementServer(nil, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	resp, err := http.Get("http://" + addr + "/api/v1/traffic")
	if err != nil {
		t.Fatalf("traffic empty: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("traffic empty status = %d, want 200", resp.StatusCode)
	}
	var snap TrafficSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snap.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(snap.Entries))
	}
}

func TestManagementBruteForceProtection(t *testing.T) {
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, false, "correct-token")
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	// 连续发送错误 token 的请求，达到 authMaxFailures 次后应被封禁
	for i := 0; i < authMaxFailures; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("request %d: status = %d, want 401", i, resp.StatusCode)
		}
	}

	// 第 authMaxFailures+1 次请求应仍返回 401（已被封禁）
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("blocked request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("blocked request status = %d, want 401", resp.StatusCode)
	}

	// 即使使用正确 token，被封禁 IP 也应被拒绝
	req2, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
	req2.Header.Set("Authorization", "Bearer correct-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("correct token after block: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("correct token after block status = %d, want 401 (IP still blocked)", resp2.StatusCode)
	}
}

func TestManagementAuthSuccessClearsFailures(t *testing.T) {
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, false, "correct-token")
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	// 发送几次错误请求（未达阈值）
	for i := 0; i < authMaxFailures-1; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}

	// 使用正确 token 应成功，并清除失败记录
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
	req.Header.Set("Authorization", "Bearer correct-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("correct token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct token status = %d, want 200", resp.StatusCode)
	}

	// 再次发送错误请求，计数应从 0 重新开始
	for i := 0; i < authMaxFailures-1; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("request %d after clear: status = %d, want 401", i, resp.StatusCode)
		}
	}

	// 此时使用正确 token 仍应成功（未达封禁阈值）
	req2, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
	req2.Header.Set("Authorization", "Bearer correct-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("correct token after partial failures: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("correct token after partial failures status = %d, want 200", resp2.StatusCode)
	}
}
