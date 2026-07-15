package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// mockConfigEditor 是测试用的 ConfigEditor 实现。
type mockConfigEditor struct {
	upstreams []UpstreamConfig
	rules     []RouteRule
	users     map[string]string
	applyErr  error
}

func newMockConfigEditor() *mockConfigEditor {
	return &mockConfigEditor{
		upstreams: []UpstreamConfig{
			{Name: "up1", URL: "socks5://user:pass@host1:1080", Types: []string{"socks5"}, Priority: 1, Weight: 5},
			{Name: "up2", URL: "http://host2:8080", Types: []string{"http"}, Priority: 2, Weight: 3},
		},
		rules: []RouteRule{
			{Type: "domain_suffix", Values: []string{"example.com"}, Action: "direct"},
			{Type: "ip_cidr", Values: []string{"10.0.0.0/8"}, Action: "direct"},
		},
		users: map[string]string{
			"alice": "secret1",
			"bob":   "secret2",
		},
	}
}

func (m *mockConfigEditor) ListUpstreams() []UpstreamConfig {
	return m.upstreams
}

func (m *mockConfigEditor) AddUpstream(up UpstreamConfig) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	for _, existing := range m.upstreams {
		if existing.Name == up.Name {
			return errors.New("upstream already exists")
		}
	}
	m.upstreams = append(m.upstreams, up)
	return nil
}

func (m *mockConfigEditor) UpdateUpstream(name string, up UpstreamConfig) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	for i, existing := range m.upstreams {
		if existing.Name == name {
			up.Name = name
			m.upstreams[i] = up
			return nil
		}
	}
	return errNotFound
}

func (m *mockConfigEditor) RemoveUpstream(name string) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	for i, existing := range m.upstreams {
		if existing.Name == name {
			m.upstreams = append(m.upstreams[:i], m.upstreams[i+1:]...)
			return nil
		}
	}
	return errNotFound
}

func (m *mockConfigEditor) ListRules() []RouteRule {
	return m.rules
}

func (m *mockConfigEditor) AddRule(rule RouteRule) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	m.rules = append(m.rules, rule)
	return nil
}

func (m *mockConfigEditor) RemoveRule(index int) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	if index < 0 || index >= len(m.rules) {
		return errNotFound
	}
	m.rules = append(m.rules[:index], m.rules[index+1:]...)
	return nil
}

func (m *mockConfigEditor) ListUsers() []string {
	result := make([]string, 0, len(m.users))
	for name := range m.users {
		result = append(result, name)
	}
	return result
}

func (m *mockConfigEditor) AddUser(username, password string) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	if m.users == nil {
		m.users = make(map[string]string)
	}
	if _, exists := m.users[username]; exists {
		return errors.New("user already exists")
	}
	m.users[username] = password
	return nil
}

func (m *mockConfigEditor) RemoveUser(username string) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	if _, exists := m.users[username]; !exists {
		return errNotFound
	}
	delete(m.users, username)
	return nil
}

// startConfigAPITestServer 启动一个带 ConfigEditor 的管理服务器用于测试。
func startConfigAPITestServer(t *testing.T, editor ConfigEditor, token string) (*ManagementServer, string) {
	t.Helper()
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, false, token)
	m.SetConfigEditor(editor)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StartListener(ln); err != nil {
		t.Fatal(err)
	}
	return m, ln.Addr().String()
}

func doRequest(t *testing.T, method, addr, path string, body any, token string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, "http://"+addr+path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var result map[string]any
	_ = json.Unmarshal(body, &result)
	return result
}

// --- Upstreams API Tests ---

func TestConfigAPIListUpstreams(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "GET", addr, "/api/v1/config/upstreams", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	result := decodeBody(t, resp)
	ups, ok := result["upstreams"].([]any)
	if !ok || len(ups) != 2 {
		t.Fatalf("expected 2 upstreams, got %v", result)
	}
	first := ups[0].(map[string]any)
	if first["name"] != "up1" {
		t.Fatalf("expected name up1, got %v", first["name"])
	}
	// URL should be redacted
	url := first["url"].(string)
	if strings.Contains(url, "pass") {
		t.Fatalf("URL should be redacted, got %s", url)
	}
}

func TestConfigAPIAddUpstream(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/upstreams", addUpstreamRequest{
		Name:  "up3",
		URL:   "socks5://host3:1080",
		Types: []string{"socks5"},
	}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if len(editor.upstreams) != 3 {
		t.Fatalf("expected 3 upstreams after add, got %d", len(editor.upstreams))
	}
	if editor.upstreams[2].Name != "up3" {
		t.Fatalf("expected name up3, got %s", editor.upstreams[2].Name)
	}
}

func TestConfigAPIAddUpstreamEmptyURL(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/upstreams", addUpstreamRequest{
		Name: "up3",
		URL:  "",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIAddUpstreamDuplicate(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/upstreams", addUpstreamRequest{
		Name: "up1",
		URL:  "socks5://host3:1080",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIUpdateUpstream(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	priority := 5
	resp := doRequest(t, "PUT", addr, "/api/v1/config/upstreams/up1", updateUpstreamRequest{
		URL:      "socks5://newhost:1080",
		Priority: &priority,
		Weight:   nil,
	}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if editor.upstreams[0].URL != "socks5://newhost:1080" {
		t.Fatalf("expected URL updated, got %s", editor.upstreams[0].URL)
	}
	if editor.upstreams[0].Priority != 5 {
		t.Fatalf("expected priority 5, got %d", editor.upstreams[0].Priority)
	}
}

func TestConfigAPIUpdateUpstreamNotFound(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "PUT", addr, "/api/v1/config/upstreams/nonexistent", updateUpstreamRequest{
		URL: "socks5://host:1080",
	}, "")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestConfigAPIRemoveUpstream(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/upstreams/up1", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(editor.upstreams) != 1 {
		t.Fatalf("expected 1 upstream after remove, got %d", len(editor.upstreams))
	}
	if editor.upstreams[0].Name != "up2" {
		t.Fatalf("expected remaining name up2, got %s", editor.upstreams[0].Name)
	}
}

func TestConfigAPIRemoveUpstreamNotFound(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/upstreams/nonexistent", nil, "")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// --- Rules API Tests ---

func TestConfigAPIListRules(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "GET", addr, "/api/v1/config/rules", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	result := decodeBody(t, resp)
	rules, ok := result["rules"].([]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %v", result)
	}
}

func TestConfigAPIAddRule(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/rules", addRuleRequest{
		Type:   "domain",
		Values: []string{"test.com"},
		Action: "proxy",
	}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if len(editor.rules) != 3 {
		t.Fatalf("expected 3 rules after add, got %d", len(editor.rules))
	}
}

func TestConfigAPIAddRuleEmptyType(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/rules", addRuleRequest{
		Type:   "",
		Action: "proxy",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIRemoveRule(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/rules/0", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(editor.rules) != 1 {
		t.Fatalf("expected 1 rule after remove, got %d", len(editor.rules))
	}
}

func TestConfigAPIRemoveRuleOutOfRange(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/rules/99", nil, "")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestConfigAPIRemoveRuleInvalidIndex(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/rules/abc", nil, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Users API Tests ---

func TestConfigAPIListUsers(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "GET", addr, "/api/v1/config/users", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	result := decodeBody(t, resp)
	users, ok := result["users"].([]any)
	if !ok || len(users) != 2 {
		t.Fatalf("expected 2 users, got %v", result)
	}
}

func TestConfigAPIAddUser(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/users", addUserRequest{
		Username: "charlie",
		Password: "pass3",
	}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if _, exists := editor.users["charlie"]; !exists {
		t.Fatal("expected user charlie to be added")
	}
}

func TestConfigAPIAddUserEmptyUsername(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/users", addUserRequest{
		Username: "",
		Password: "pass",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIAddUserEmptyPassword(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/users", addUserRequest{
		Username: "newuser",
		Password: "",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIAddUserDuplicate(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/users", addUserRequest{
		Username: "alice",
		Password: "newpass",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIRemoveUser(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/users/alice", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if _, exists := editor.users["alice"]; exists {
		t.Fatal("expected user alice to be removed")
	}
}

func TestConfigAPIRemoveUserNotFound(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "DELETE", addr, "/api/v1/config/users/nonexistent", nil, "")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// --- Auth & Error Tests ---

func TestConfigAPIRequiresAuth(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "secret-token")
	defer m.Stop()

	resp := doRequest(t, "GET", addr, "/api/v1/config/upstreams", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doRequest(t, "GET", addr, "/api/v1/config/upstreams", nil, "secret-token")
	if resp.StatusCode != 200 {
		t.Fatalf("status with token = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestConfigAPINotConfigured(t *testing.T) {
	m := NewManagementServer(func() HealthStatus {
		return HealthStatus{Status: "up", RouterReady: true, ManagerReady: true}
	}, false)
	if err := m.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	addr := m.ln.Addr().String()

	resp := doRequest(t, "GET", addr, "/api/v1/config/upstreams", nil, "")
	if resp.StatusCode != 501 {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestConfigAPIUnknownPath(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "GET", addr, "/api/v1/config/unknown", nil, "")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestConfigAPIMethodNotAllowed(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "PATCH", addr, "/api/v1/config/upstreams", nil, "")
	if resp.StatusCode != 405 {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestConfigAPIInvalidJSON(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	req, _ := http.NewRequest("POST", "http://"+addr+"/api/v1/config/upstreams", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestConfigAPIUnknownFieldInJSON(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	req, _ := http.NewRequest("POST", "http://"+addr+"/api/v1/config/upstreams", strings.NewReader(`{"name":"up3","url":"socks5://h:1080","unknown_field":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (unknown field should be rejected)", resp.StatusCode)
	}
}

func TestConfigAPIApplyError(t *testing.T) {
	editor := newMockConfigEditor()
	editor.applyErr = errors.New("disk full")
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "POST", addr, "/api/v1/config/upstreams", addUpstreamRequest{
		Name: "up3",
		URL:  "socks5://host3:1080",
	}, "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	result := decodeBody(t, resp)
	if !strings.Contains(result["error"].(string), "disk full") {
		t.Fatalf("expected error to contain 'disk full', got %v", result["error"])
	}
}

func TestConfigAPIRedactURL(t *testing.T) {
	editor := &mockConfigEditor{
		upstreams: []UpstreamConfig{
			{Name: "secret", URL: "socks5://admin:supersecret@10.0.0.1:1080"},
		},
	}
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	resp := doRequest(t, "GET", addr, "/api/v1/config/upstreams", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "supersecret") {
		t.Fatalf("password should be redacted in response, got %s", body)
	}
}

func TestConfigAPIBodySizeLimit(t *testing.T) {
	editor := newMockConfigEditor()
	m, addr := startConfigAPITestServer(t, editor, "")
	defer m.Stop()

	// 构造一个超过 1 MiB 的请求体
	largeName := strings.Repeat("a", 2*1024*1024)
	resp := doRequest(t, "POST", addr, "/api/v1/config/upstreams", addUpstreamRequest{
		Name: largeName,
		URL:  "socks5://host:1080",
	}, "")
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 for oversized body", resp.StatusCode)
	}
}
