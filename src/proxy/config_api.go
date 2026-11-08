package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
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
	// UpdateRule 按索引更新路由规则并持久化。
	UpdateRule(index int, rule RouteRule) error
	// MoveRule 按索引上移/下移路由规则并持久化。
	MoveRule(index int, direction string) error
	// RemoveRule 按索引删除路由规则并持久化。
	RemoveRule(index int) error

	// ListUsers 返回当前用户名列表（不含密码）。
	ListUsers() []string
	// AddUser 添加用户并持久化。
	AddUser(username, password string) error
	// UpdateUser 修改用户密码并持久化。
	UpdateUser(username, password string) error
	// RemoveUser 按用户名删除用户并持久化。
	RemoveUser(username string) error

	// GetSettings 返回全局设置视图。
	GetSettings() (SettingsView, error)
	// UpdateSettings 部分更新全局设置并持久化。
	UpdateSettings(patch SettingsPatch) error
}

// healthCheckInfo 是健康检查配置的 API 表示（时间单位为秒）。
type healthCheckInfo struct {
	Type             string  `json:"type"`
	Path             string  `json:"path,omitempty"`
	ExpectedCodes    []int   `json:"expected_codes,omitempty"`
	IntervalSeconds  float64 `json:"interval_seconds"`
	TimeoutSeconds   float64 `json:"timeout_seconds"`
	FailureThreshold int     `json:"failure_threshold"`
	SuccessThreshold int     `json:"success_threshold"`
}

// healthCheckRequest 是健康检查配置的写入请求（时间单位为秒）。
type healthCheckRequest struct {
	Type             string  `json:"type"`
	Path             string  `json:"path"`
	ExpectedCodes    []int   `json:"expected_codes"`
	IntervalSeconds  float64 `json:"interval_seconds"`
	TimeoutSeconds   float64 `json:"timeout_seconds"`
	FailureThreshold int     `json:"failure_threshold"`
	SuccessThreshold int     `json:"success_threshold"`
}

func newHealthCheckInfo(hc HealthCheckConfig) healthCheckInfo {
	return healthCheckInfo{
		Type:             hc.Type,
		Path:             hc.Path,
		ExpectedCodes:    hc.ExpectedCodes,
		IntervalSeconds:  time.Duration(hc.Interval).Seconds(),
		TimeoutSeconds:   time.Duration(hc.Timeout).Seconds(),
		FailureThreshold: hc.FailureThreshold,
		SuccessThreshold: hc.SuccessThreshold,
	}
}

func (h healthCheckRequest) toConfig() HealthCheckConfig {
	hc := HealthCheckConfig{
		Type:             strings.TrimSpace(h.Type),
		Path:             strings.TrimSpace(h.Path),
		ExpectedCodes:    h.ExpectedCodes,
		FailureThreshold: h.FailureThreshold,
		SuccessThreshold: h.SuccessThreshold,
	}
	if h.IntervalSeconds > 0 {
		hc.Interval = Duration(time.Duration(h.IntervalSeconds * float64(time.Second)))
	}
	if h.TimeoutSeconds > 0 {
		hc.Timeout = Duration(time.Duration(h.TimeoutSeconds * float64(time.Second)))
	}
	return hc
}

// upstreamInfo 是上游的 API 响应表示，URL 中的凭证被脱敏。
type upstreamInfo struct {
	Name        string          `json:"name"`
	URL         string          `json:"url"`
	Enabled     bool            `json:"enabled"`
	Types       []string        `json:"types,omitempty"`
	Priority    int             `json:"priority"`
	Weight      int             `json:"weight"`
	Strategy    string          `json:"strategy"`
	HealthCheck healthCheckInfo `json:"health_check"`
}

// addUpstreamRequest 是添加上游的请求体。
type addUpstreamRequest struct {
	Name        string              `json:"name"`
	URL         string              `json:"url"`
	Enabled     *bool               `json:"enabled,omitempty"`
	Types       []string            `json:"types,omitempty"`
	Priority    int                 `json:"priority,omitempty"`
	Weight      int                 `json:"weight,omitempty"`
	Strategy    string              `json:"strategy,omitempty"`
	HealthCheck *healthCheckRequest `json:"health_check,omitempty"`
}

// updateUpstreamRequest 是更新上游的请求体。
// 未提供的字段（nil / 空）保持原值，便于 WebUI 做局部更新。
type updateUpstreamRequest struct {
	Name        string              `json:"name,omitempty"` // 非空且与路径中旧名称不同时执行改名
	URL         string              `json:"url"`
	Enabled     *bool               `json:"enabled,omitempty"`
	Types       []string            `json:"types,omitempty"`
	Priority    *int                `json:"priority,omitempty"`
	Weight      *int                `json:"weight,omitempty"`
	Strategy    string              `json:"strategy,omitempty"`
	HealthCheck *healthCheckRequest `json:"health_check,omitempty"`
}

// moveRuleRequest 是规则排序请求体。
type moveRuleRequest struct {
	Direction string `json:"direction"` // up / down
}

// updateUserRequest 是修改用户密码的请求体。
type updateUserRequest struct {
	Password string `json:"password"`
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
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		writeJSONError(w, http.StatusNotFound, "config API path required")
		return
	}
	segments := strings.Split(path, "/")

	switch segments[0] {
	case "upstreams":
		m.handleUpstreamsAPI(w, r, editor, segments[1:])
	case "rules":
		m.handleRulesAPI(w, r, editor, segments[1:])
	case "users":
		m.handleUsersAPI(w, r, editor, segments[1:])
	case "settings":
		if len(segments) > 1 {
			writeJSONError(w, http.StatusNotFound, "unknown config API path")
			return
		}
		m.handleSettingsAPI(w, r, editor)
	default:
		writeJSONError(w, http.StatusNotFound, "unknown config API path")
	}
}

func (m *ManagementServer) handleUpstreamsAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor, rest []string) {
	MetricInc("yigate_management_requests_total", "path=/api/v1/config/upstreams")
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
				Enabled:     up.IsEnabled(),
				Types:       up.Types,
				Priority:    up.Priority,
				Weight:      up.Weight,
				Strategy:    normalizeStrategy(up.Strategy),
				HealthCheck: newHealthCheckInfo(up.HealthCheck),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"upstreams": result})

	case http.MethodPost:
		switch len(rest) {
		case 0:
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
				Name:     strings.TrimSpace(req.Name),
				URL:      req.URL,
				Enabled:  req.Enabled,
				Types:    req.Types,
				Priority: req.Priority,
				Weight:   req.Weight,
				Strategy: req.Strategy,
			}
			if req.HealthCheck != nil {
				up.HealthCheck = req.HealthCheck.toConfig()
			}
			if err := editor.AddUpstream(up); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]string{"status": "ok", "name": up.Name})
		case 1:
			if rest[0] != "check" {
				writeJSONError(w, http.StatusNotFound, "unknown path")
				return
			}
			if m.checkAllUpstreams == nil {
				writeJSONError(w, http.StatusServiceUnavailable, "upstream checker not ready")
				return
			}
			results := m.checkAllUpstreams()
			writeJSON(w, http.StatusOK, map[string]any{"results": results})
		case 2:
			if rest[1] != "check" {
				writeJSONError(w, http.StatusNotFound, "unknown path")
				return
			}
			if m.checkUpstream == nil {
				writeJSONError(w, http.StatusServiceUnavailable, "upstream checker not ready")
				return
			}
			res, err := m.checkUpstream(rest[0])
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"result": res})
		default:
			writeJSONError(w, http.StatusNotFound, "unknown path")
		}

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
		newName := strings.TrimSpace(req.Name)
		if newName == "" {
			newName = name
		}
		// 局部更新：未提供的字段沿用已有配置，避免编辑弹窗抹掉
		// 健康检查、类型、策略等未在表单中展示的字段。
		up := UpstreamConfig{
			Name:     newName,
			URL:      req.URL,
			Types:    req.Types,
			Strategy: req.Strategy,
		}
		if existing, ok := findUpstream(editor.ListUpstreams(), name); ok {
			if up.URL == "" {
				up.URL = existing.URL
			}
			up.Enabled = existing.Enabled
			if up.Types == nil {
				up.Types = existing.Types
			}
			if up.Strategy == "" {
				up.Strategy = existing.Strategy
			}
			up.HealthCheck = existing.HealthCheck
			up.Priority = existing.Priority
			up.Weight = existing.Weight
		}
		if req.Priority != nil {
			up.Priority = *req.Priority
		}
		if req.Weight != nil {
			up.Weight = *req.Weight
		}
		if req.HealthCheck != nil {
			up.HealthCheck = req.HealthCheck.toConfig()
		}
		if req.Enabled != nil {
			up.Enabled = req.Enabled
		}
		if err := editor.UpdateUpstream(name, up); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": newName})

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
	MetricInc("yigate_management_requests_total", "path=/api/v1/config/rules")
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
		// POST /api/v1/config/rules/{index}/move 调整规则顺序
		if len(rest) == 2 && rest[1] == "move" {
			index, err := strconv.Atoi(rest[0])
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid rule index")
				return
			}
			var req moveRuleRequest
			if err := decodeJSON(r, &req); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := editor.MoveRule(index, req.Direction); err != nil {
				if errors.Is(err, errNotFound) {
					writeJSONError(w, http.StatusNotFound, err.Error())
				} else {
					writeJSONError(w, http.StatusBadRequest, err.Error())
				}
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
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
		rule := RouteRule(req)
		if err := editor.AddRule(rule); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})

	case http.MethodPut:
		if len(rest) != 1 {
			writeJSONError(w, http.StatusBadRequest, "rule index required in path")
			return
		}
		index, err := strconv.Atoi(rest[0])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid rule index")
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
		rule := RouteRule(req)
		if err := editor.UpdateRule(index, rule); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

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
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (m *ManagementServer) handleUsersAPI(w http.ResponseWriter, r *http.Request, editor ConfigEditor, rest []string) {
	MetricInc("yigate_management_requests_total", "path=/api/v1/config/users")
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

	case http.MethodPut:
		if len(rest) != 1 {
			writeJSONError(w, http.StatusBadRequest, "username required in path")
			return
		}
		username := rest[0]
		var req updateUserRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(req.Password) == "" {
			writeJSONError(w, http.StatusBadRequest, "password must not be empty")
			return
		}
		if err := editor.UpdateUser(username, req.Password); err != nil {
			if errors.Is(err, errNotFound) {
				writeJSONError(w, http.StatusNotFound, err.Error())
			} else {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "username": username})

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
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// findUpstream 在列表中按名称查找上游。
func findUpstream(ups []UpstreamConfig, name string) (UpstreamConfig, bool) {
	for _, up := range ups {
		if strings.TrimSpace(up.Name) == name {
			return up, true
		}
	}
	return UpstreamConfig{}, false
}

// ErrNotFound 表示资源不存在，用于区分 404 和 400 响应。
// 配置编辑器实现位于 main 包，必须复用同一个哨兵值才能让 errors.Is 生效。
var ErrNotFound = errors.New("not found")

// errNotFound 保留包内别名，便于既有代码与测试引用。
var errNotFound = ErrNotFound

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError 写入 JSON 错误响应。
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	// MaxBytesReader 超限使用标准库固定文案，统一映射为 413 并对调用方隐藏内部路径。
	if strings.Contains(msg, "http: request body too large") {
		code = http.StatusRequestEntityTooLarge
		msg = "request body too large"
	}
	if code >= http.StatusInternalServerError {
		// 5xx 详情只写日志，避免把临时文件/配置绝对路径回显给调用者。
		Logger().Errorw("management request failed", "status", code, "error", msg)
		msg = "internal server error"
	}
	writeJSON(w, code, map[string]string{"status": "error", "error": msg})
}

// maxManagementBodySize 限制管理 API 请求体大小（1 MiB）。
const maxManagementBodySize = 1 << 20

// decodeJSON 解码 JSON 请求体，限制最大大小以防止 DoS。
func decodeJSON(r *http.Request, v any) error {
	// MaxBytesReader 会消费并关闭超限请求体，避免 keep-alive 连接上的残留字节
	// 被解析成下一个请求；writeJSONError 会把对应错误统一映射为 413。
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxManagementBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
