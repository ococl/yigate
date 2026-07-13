package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ConfigEditor 提供运行时配置变更能力。
// 所有方法必须线程安全，且变更需持久化到磁盘并触发热加载。
type ConfigEditor interface {
	// ListUpstreams 返回当前上游列表。
	ListUpstreams() []UpstreamConfig
	// AddUpstream 添加新上游并持久化。
	AddUpstream(up UpstreamConfig) error
	// UpdateUpstream 按名称更新已有上游并持久化。
	UpdateUpstream(name string, up UpstreamConfig) error
	// RemoveUpstream 按名称删除上游并持久化。
	RemoveUpstream(name string) error

	// ListRules 返回当前路由规则列表。
	ListRules() []RouteRule
	// AddRule 在末尾添加路由规则并持久化。
	AddRule(rule RouteRule) error
	// RemoveRule 按索引删除路由规则并持久化。
	RemoveRule(index int) error

	// ListUsers 返回当前用户名列表（不含密码）。
	ListUsers() []string
	// AddUser 添加用户并持久化。
	AddUser(username, password string) error
	// RemoveUser 按用户名删除用户并持久化。
	RemoveUser(username string) error
}

// upstreamInfo 是上游的 API 响应表示，URL 中的凭证被脱敏。
type upstreamInfo struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Types       []string `json:"types,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Weight      int      `json:"weight,omitempty"`
	Strategy    string   `json:"strategy,omitempty"`
	HealthCheck string   `json:"health_check_type,omitempty"`
}

// addUpstreamRequest 是添加上游的请求体。
type addUpstreamRequest struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Types    []string `json:"types,omitempty"`
	Priority int      `json:"priority,omitempty"`
	Weight   int      `json:"weight,omitempty"`
	Strategy string   `json:"strategy,omitempty"`
}

// updateUpstreamRequest 是更新上游的请求体。
type updateUpstreamRequest struct {
	URL      string   `json:"url"`
	Types    []string `json:"types,omitempty"`
	Priority *int     `json:"priority,omitempty"`
	Weight   *int     `json:"weight,omitempty"`
	Strategy string   `json:"strategy,omitempty"`
}

// ruleInfo 是路由规则的 API 响应表示。
type ruleInfo struct {
	Index     int      `json:"index"`
	Type      string   `json:"type"`
	Values    []string `json:"values,omitempty"`
	Action    string   `json:"action"`
	Upstreams []string `json:"upstreams,omitempty"`
}

// addRuleRequest 是添加路由规则的请求体。
type addRuleRequest struct {
	Type      string   `json:"type"`
	Values    []string `json:"values,omitempty"`
	Action    string   `json:"action"`
	Upstreams []string `json:"upstreams,omitempty"`
}

// userInfo 是用户的 API 响应表示。
type userInfo struct {
	Username string `json:"username"`
}

// addUserRequest 是添加用户的请求体。
type addUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SetConfigEditor 设置运行时配置编辑器。
// 设置后，/api/v1/config/* 端点将可用。
func (m *ManagementServer) SetConfigEditor(editor ConfigEditor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configEditor = editor
}

func (m *ManagementServer) configEditorSafe() ConfigEditor {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.configEditor
}

// handleConfigAPI 处理 /api/v1/config/* 路径的请求。
func (m *ManagementServer) handleConfigAPI(w http.ResponseWriter, r *http.Request) {
	if !m.requireAuth(w, r) {
		return
	}

	editor := m.configEditorSafe()
	if editor == nil {
		writeJSONError(w, http.StatusNotImplemented, "config API not configured")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/config")
	path = strings.TrimPrefix(path, "/")
	segments := strings.Split(path, "/")
	if path == "" {
		segments = []string{}
	}

	switch segments[0] {
	case "upstreams":
		m.handleUpstreamsAPI(w, r, editor, segments[1:])
	case "rules":
		m.handleRulesAPI(w, r, editor, segments[1:])
	case "users":
		m.handleUsersAPI(w, r, editor, segments[1:])
	default:
		writeJSONError(w, http.StatusNotFound, "unknown config API path")
	}
}

func (m *ManagementServer) handleUpstreamsAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor, rest []string) {
	MetricInc("k5pw_management_requests_total", "path=/api/v1/config/upstreams")
	switch r.Method {
	case http.MethodGet:
		if len(rest) > 0 {
			writeJSONError(w, http.StatusNotFound, "unknown path")
			return
		}
		ups := editor.ListUpstreams()
		result := make([]upstreamInfo, 0, len(ups))
		for _, up := range ups {
			result = append(result, upstreamInfo{
				Name:        up.Name,
				URL:         redactURLString(up.URL),
				Types:       up.Types,
				Priority:    up.Priority,
				Weight:      up.Weight,
				Strategy:    up.Strategy,
				HealthCheck: up.HealthCheck.Type,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"upstreams": result})

	case http.MethodPost:
		if len(rest) > 0 {
			writeJSONError(w, http.StatusNotFound, "unknown path")
			return
		}
		var req addUpstreamRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			writeJSONError(w, http.StatusBadRequest, "url must not be empty")
			return
		}
		up := UpstreamConfig{
			Name:     req.Name,
			URL:      req.URL,
			Types:    req.Types,
			Priority: req.Priority,
			Weight:   req.Weight,
			Strategy: req.Strategy,
		}
		if err := editor.AddUpstream(up); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "name": up.Name})

	case http.MethodPut:
		if len(rest) != 1 {
			writeJSONError(w, http.StatusBadRequest, "upstream name required in path")
			return
		}
		name := rest[0]
		var req updateUpstreamRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		up := UpstreamConfig{
			Name:     name,
			URL:      req.URL,
			Types:    req.Types,
			Strategy: req.Strategy,
		}
		if req.Priority != nil {
			up.Priority = *req.Priority
		}
		if req.Weight != nil {
			up.Weight = *req.Weight
		}
		if err := editor.UpdateUpstream(name, up); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": name})

	case http.MethodDelete:
		if len(rest) != 1 {
			writeJSONError(w, http.StatusBadRequest, "upstream name required in path")
			return
		}
		name := rest[0]
		if err := editor.RemoveUpstream(name); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": name})

	default:
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (m *ManagementServer) handleRulesAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor, rest []string) {
	MetricInc("k5pw_management_requests_total", "path=/api/v1/config/rules")
	switch r.Method {
	case http.MethodGet:
		if len(rest) > 0 {
			writeJSONError(w, http.StatusNotFound, "unknown path")
			return
		}
		rules := editor.ListRules()
		result := make([]ruleInfo, 0, len(rules))
		for i, rule := range rules {
			result = append(result, ruleInfo{
				Index:     i,
				Type:      rule.Type,
				Values:    rule.Values,
				Action:    rule.Action,
				Upstreams: rule.Upstreams,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": result})

	case http.MethodPost:
		if len(rest) > 0 {
			writeJSONError(w, http.StatusNotFound, "unknown path")
			return
		}
		var req addRuleRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(req.Type) == "" {
			writeJSONError(w, http.StatusBadRequest, "type must not be empty")
			return
		}
		if strings.TrimSpace(req.Action) == "" {
			writeJSONError(w, http.StatusBadRequest, "action must not be empty")
			return
		}
		rule := RouteRule{
			Type:      req.Type,
			Values:    req.Values,
			Action:    req.Action,
			Upstreams: req.Upstreams,
		}
		if err := editor.AddRule(rule); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})

	case http.MethodDelete:
		if len(rest) != 1 {
			writeJSONError(w, http.StatusBadRequest, "rule index required in path")
			return
		}
		index, err := strconv.Atoi(rest[0])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid rule index")
			return
		}
		if err := editor.RemoveRule(index); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "index": rest[0]})

	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (m *ManagementServer) handleUsersAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor, rest []string) {
	MetricInc("k5pw_management_requests_total", "path=/api/v1/config/users")
	switch r.Method {
	case http.MethodGet:
		if len(rest) > 0 {
			writeJSONError(w, http.StatusNotFound, "unknown path")
			return
		}
		usernames := editor.ListUsers()
		result := make([]userInfo, 0, len(usernames))
		for _, name := range usernames {
			result = append(result, userInfo{Username: name})
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": result})

	case http.MethodPost:
		if len(rest) > 0 {
			writeJSONError(w, http.StatusNotFound, "unknown path")
			return
		}
		var req addUserRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(req.Username) == "" {
			writeJSONError(w, http.StatusBadRequest, "username must not be empty")
			return
		}
		if strings.TrimSpace(req.Password) == "" {
			writeJSONError(w, http.StatusBadRequest, "password must not be empty")
			return
		}
		if err := editor.AddUser(req.Username, req.Password); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "username": req.Username})

	case http.MethodDelete:
		if len(rest) != 1 {
			writeJSONError(w, http.StatusBadRequest, "username required in path")
			return
		}
		username := rest[0]
		if err := editor.RemoveUser(username); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "username": username})

	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// errNotFound 表示资源不存在，用于区分 404 和 400 响应。
var errNotFound = errors.New("not found")

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError 写入 JSON 错误响应。
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"status": "error", "error": msg})
}

// decodeJSON 解码 JSON 请求体。
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// redactUpstreamURL 脱敏上游 URL 中的凭证信息。
func redactUpstreamURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid>"
	}
	return u.Redacted()
}
