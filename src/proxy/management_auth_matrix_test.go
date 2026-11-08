package proxy

import (
	"net/http"
	"testing"
)

// TestManagementAuthMatrix 逐路由验证管理接口鉴权覆盖，防止新增 handler 忘记 requireAuth。
// WebUI 静态资源是登录入口，设计上匿名可读，但必须带基础安全响应头。
func TestManagementAuthMatrix(t *testing.T) {
	const token = "matrix-secret-token"
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, true, token)
	m.SetStatusSnapshotGetter(func() StatusSnapshot { return StatusSnapshot{Status: "up"} })
	m.SetReload(func() error { return nil })
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	protected := []string{
		"/health",
		"/metrics",
		"/api/v1/status",
		"/api/v1/version",
		"/api/v1/traffic",
		"/api/v1/config/upstreams",
		"/api/v1/geosite",
		"/debug/pprof/",
	}
	for _, path := range protected {
		// 带令牌的请求先清除失败计数，避免连续 401 触发单 IP 封禁影响后续断言。
		authReq, _ := http.NewRequest(http.MethodGet, "http://"+addr+path, nil)
		authReq.Header.Set("Authorization", "Bearer "+token)
		authResp, err := http.DefaultClient.Do(authReq)
		if err != nil {
			t.Fatalf("%s authenticated request: %v", path, err)
		}
		authResp.Body.Close()
		if authResp.StatusCode == http.StatusUnauthorized {
			t.Fatalf("%s rejected a valid token", path)
		}

		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatalf("%s unauthenticated request: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without token = %d, want 401", path, resp.StatusCode)
		}
	}

	// 静态资源有意公开（否则登录页无法加载），但不得返回鉴权错误且需带安全头。
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("webui root request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("webui static assets must stay reachable before login")
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Fatalf("webui %s = %q, want %q", header, got, want)
		}
	}
}
