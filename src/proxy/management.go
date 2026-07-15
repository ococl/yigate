package proxy

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"time"
)

// HealthStatus 表示进程健康状态，用于 /health 端点返回。
type HealthStatus struct {
	Status        string    `json:"status"`
	Listen        string    `json:"listen"`
	UpstreamCount int       `json:"upstream_count"`
	RouterReady   bool      `json:"router_ready"`
	ManagerReady  bool      `json:"manager_ready"`
	Timestamp     time.Time `json:"timestamp"`
}

// StatusSnapshot 用于 /api/v1/status 返回的进程运行摘要。
type StatusSnapshot struct {
	Status        string    `json:"status"`
	Listen        string    `json:"listen"`
	Management    string    `json:"management"`
	UpstreamCount int       `json:"upstream_count"`
	RouterReady   bool      `json:"router_ready"`
	ManagerReady  bool      `json:"manager_ready"`
	PprofEnabled  bool      `json:"pprof_enabled"`
	Timestamp     time.Time `json:"timestamp"`
}

// ManagementServer 提供管理接口，如健康检查、指标、状态与手动重载等。
// 管理端口与代理端口分离，避免影响协议识别。
type ManagementServer struct {
	getStatus         func() HealthStatus
	getStatusSnapshot func() StatusSnapshot
	reload            func() error
	enablePprof       bool
	authToken         string
	configEditor      ConfigEditor
	mu                sync.Mutex
	ln                net.Listener
	srv               *http.Server
	pprofHandlers     map[string]http.Handler

	// authFailures 跟踪每 IP 的认证失败次数，用于暴力破解防护。
	authFailures   map[string]*authFailState
	authFailuresMu sync.Mutex
}

// authFailState 记录单个 IP 的认证失败状态。
type authFailState struct {
	count        int
	blockedUntil time.Time
}

const (
	// authMaxFailures 是单 IP 连续认证失败的最大次数，超过后临时封禁。
	authMaxFailures = 10
	// authBlockDuration 是认证失败封禁持续时间。
	authBlockDuration = 5 * time.Minute
)

// NewManagementServer 创建管理服务器，getStatus 提供当前健康状态。
// enablePprof 为 true 时注册 /debug/pprof 路由，用于线上问题排查。
// authToken 非空时，所有管理端点都要求 Authorization: Bearer <token>。
func NewManagementServer(getStatus func() HealthStatus, enablePprof bool, authToken ...string) *ManagementServer {
	token := ""
	if len(authToken) > 0 {
		token = strings.TrimSpace(authToken[0])
	}
	m := &ManagementServer{
		getStatus:    getStatus,
		enablePprof:  enablePprof,
		authToken:    token,
		authFailures: make(map[string]*authFailState),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.handleHealth)
	mux.HandleFunc("/metrics", m.handleMetrics)
	mux.HandleFunc("/api/v1/status", m.handleStatus)
	mux.HandleFunc("/api/v1/reload", m.handleReload)
	mux.HandleFunc("/api/v1/config/", m.handleConfigAPI)
	mux.HandleFunc("/api/v1/traffic", m.handleTraffic)
	if enablePprof {
		m.pprofHandlers = map[string]http.Handler{
			"/debug/pprof/":             http.HandlerFunc(pprof.Index),
			"/debug/pprof/cmdline":      http.HandlerFunc(pprof.Cmdline),
			"/debug/pprof/profile":      http.HandlerFunc(pprof.Profile),
			"/debug/pprof/symbol":       http.HandlerFunc(pprof.Symbol),
			"/debug/pprof/trace":        http.HandlerFunc(pprof.Trace),
			"/debug/pprof/allocs":       pprof.Handler("allocs"),
			"/debug/pprof/block":        pprof.Handler("block"),
			"/debug/pprof/goroutine":    pprof.Handler("goroutine"),
			"/debug/pprof/heap":         pprof.Handler("heap"),
			"/debug/pprof/mutex":        pprof.Handler("mutex"),
			"/debug/pprof/threadcreate": pprof.Handler("threadcreate"),
		}
		for path, h := range m.pprofHandlers {
			mux.Handle(path, m.authenticatedHandler(h))
		}
	}
	m.srv = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return m
}

func (m *ManagementServer) AuthToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.authToken
}

func (m *ManagementServer) checkAuth(r *http.Request) bool {
	m.mu.Lock()
	token := m.authToken
	m.mu.Unlock()
	if token == "" {
		return true
	}

	// 暴力破解防护：检查 IP 是否被封禁
	clientIP := clientIPFromRequest(r)
	if clientIP != "" {
		m.authFailuresMu.Lock()
		state, ok := m.authFailures[clientIP]
		if ok && time.Now().Before(state.blockedUntil) {
			m.authFailuresMu.Unlock()
			return false
		}
		m.authFailuresMu.Unlock()
	}

	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		m.recordAuthFailure(clientIP)
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if provided == "" || len(provided) != len(token) {
		m.recordAuthFailure(clientIP)
		return false
	}
	ok := subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
	if !ok {
		m.recordAuthFailure(clientIP)
	} else {
		m.clearAuthFailures(clientIP)
	}
	return ok
}

// recordAuthFailure 记录一次认证失败，超过阈值后临时封禁该 IP。
func (m *ManagementServer) recordAuthFailure(ip string) {
	if ip == "" {
		return
	}
	m.authFailuresMu.Lock()
	defer m.authFailuresMu.Unlock()
	state, ok := m.authFailures[ip]
	if !ok {
		state = &authFailState{}
		m.authFailures[ip] = state
	}
	state.count++
	if state.count >= authMaxFailures {
		state.blockedUntil = time.Now().Add(authBlockDuration)
	}
}

// clearAuthFailures 清除指定 IP 的认证失败记录。
func (m *ManagementServer) clearAuthFailures(ip string) {
	if ip == "" {
		return
	}
	m.authFailuresMu.Lock()
	delete(m.authFailures, ip)
	m.authFailuresMu.Unlock()
}

// clientIPFromRequest 从请求中提取客户端 IP 地址。
func clientIPFromRequest(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (m *ManagementServer) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if m.checkAuth(r) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="k5pw-management"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "unauthorized"})
	return false
}

func (m *ManagementServer) authenticatedHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.requireAuth(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SetPprofEnabled 动态启用或禁用 pprof 端点。需要重启管理服务器生效。
func (m *ManagementServer) SetPprofEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enablePprof = enabled
}

// PprofEnabled 返回当前是否启用了 pprof。
func (m *ManagementServer) PprofEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enablePprof
}

// SetStatusGetter 动态更新状态获取函数，用于热加载后切换回调。
func (m *ManagementServer) SetStatusGetter(getStatus func() HealthStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getStatus = getStatus
}

// SetStatusSnapshotGetter 设置 /api/v1/status 返回的运行摘要回调。
func (m *ManagementServer) SetStatusSnapshotGetter(getStatus func() StatusSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getStatusSnapshot = getStatus
}

// SetReload 设置手动重载配置的回调。
func (m *ManagementServer) SetReload(reload func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reload = reload
}

// Start 在指定地址启动管理接口。若 addr 为空则不会监听。
func (m *ManagementServer) Start(addr string) error {
	if addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if err := m.StartListener(ln); err != nil {
		_ = ln.Close()
		return err
	}
	return nil
}

// StartListener 使用已经绑定的监听器启动管理接口。
// 启动成功后监听器所有权转移给 ManagementServer。
func (m *ManagementServer) StartListener(ln net.Listener) error {
	if ln == nil {
		return errors.New("management listener is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ln != nil {
		return errors.New("management server already started")
	}
	m.ln = ln
	go func() {
		if err := m.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			Logger().Warnw("management server error", "error", err)
		}
	}()
	Logger().Infow("management server started", "address", ln.Addr().String(), "pprof", m.enablePprof)
	return nil
}

// Addr 返回当前监听地址。若未启动返回空字符串。
func (m *ManagementServer) Addr() string {
	m.mu.Lock()
	ln := m.ln
	m.mu.Unlock()
	if ln == nil {
		return ""
	}
	return ln.Addr().String()
}

// Stop 关闭管理接口。
func (m *ManagementServer) Stop() {
	m.mu.Lock()
	ln := m.ln
	m.ln = nil
	m.mu.Unlock()
	if ln == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.srv.Shutdown(ctx); err != nil {
		Logger().Warnw("management server shutdown error", "error", err)
	}
}

func (m *ManagementServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !m.requireAuth(w, r) {
		return
	}
	MetricInc("k5pw_management_requests_total", "path=/health")
	m.mu.Lock()
	getStatus := m.getStatus
	m.mu.Unlock()
	status := HealthStatus{Status: "up"}
	if getStatus != nil {
		status = getStatus()
	}
	status.Timestamp = time.Now()
	w.Header().Set("Content-Type", "application/json")
	if status.Status == "" {
		status.Status = "up"
	}
	code := http.StatusOK
	if !status.RouterReady || !status.ManagerReady {
		code = http.StatusServiceUnavailable
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}

func (m *ManagementServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !m.requireAuth(w, r) {
		return
	}
	MetricInc("k5pw_management_requests_total", "path=/metrics")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	ExposeMetrics(w)
}

func (m *ManagementServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !m.requireAuth(w, r) {
		return
	}
	MetricInc("k5pw_management_requests_total", "path=/api/v1/status")
	m.mu.Lock()
	getSnapshot := m.getStatusSnapshot
	getStatus := m.getStatus
	m.mu.Unlock()

	var snapshot StatusSnapshot
	if getSnapshot != nil {
		snapshot = getSnapshot()
	} else if getStatus != nil {
		hs := getStatus()
		snapshot = StatusSnapshot{
			Status:        hs.Status,
			Listen:        hs.Listen,
			UpstreamCount: hs.UpstreamCount,
			RouterReady:   hs.RouterReady,
			ManagerReady:  hs.ManagerReady,
			PprofEnabled:  m.PprofEnabled(),
		}
	}
	snapshot.Timestamp = time.Now()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(snapshot)
}

func (m *ManagementServer) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !m.requireAuth(w, r) {
		return
	}
	MetricInc("k5pw_management_requests_total", "path=/api/v1/reload")
	m.mu.Lock()
	reload := m.reload
	m.mu.Unlock()
	if reload == nil {
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "reload not configured"})
		return
	}
	if err := reload(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *ManagementServer) handleTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !m.requireAuth(w, r) {
		return
	}
	MetricInc("k5pw_management_requests_total", "path=/api/v1/traffic")
	snapshot := TrafficSnapshot{}
	if globalTrafficAccounter != nil {
		snapshot = globalTrafficAccounter.Snapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(snapshot)
}
