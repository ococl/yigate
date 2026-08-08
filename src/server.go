package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"yigate/proxy"
)

type listenerRuntime struct {
	config ListenerConfig
	access *proxy.AccessController
}

type runtimeState struct {
	config           *Config
	manager          *proxy.UpstreamManager
	router           *proxy.Router
	geoUpdate        *proxy.GeoSiteUpdater
	listeners        map[string]*listenerRuntime
	trafficAccounter *proxy.TrafficAccounter
}

// listenerContext 封装单个 listener 的运行时上下文，供连接处理使用。
type listenerContext struct {
	config ListenerConfig
	access *proxy.AccessController
}

// Server 统一代理服务器，支持配置热加载
type Server struct {
	cfgPath     string
	runtime     atomic.Pointer[runtimeState]
	mgmt        *proxy.ManagementServer
	mgmtAddr    string
	stateMu     sync.RWMutex
	applyMu     sync.Mutex
	listenersMu sync.Mutex
	listeners   map[string]net.Listener
	quit        chan struct{}
	stopOnce    sync.Once
	shutdownErr error
	activeConns sync.WaitGroup
	activeMu    sync.Mutex
	connections map[net.Conn]struct{}
	stopping    bool
	watcherMu   sync.Mutex
	watcher     *fsnotify.Watcher
	watcherWG   sync.WaitGroup
	connCount   atomic.Int64
	maxConns    atomic.Int64
}

// NewServer 创建服务器，cfg 为初始配置
func NewServer(cfgPath string, cfg *Config) (*Server, error) {
	s := &Server{
		cfgPath:     cfgPath,
		quit:        make(chan struct{}),
		connections: make(map[net.Conn]struct{}),
		listeners:   make(map[string]net.Listener),
	}
	if err := s.ApplyConfig(cfg); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) managementServer() *proxy.ManagementServer {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.mgmt
}

// statusSnapshot 返回当前进程运行摘要，供管理 API 使用。
func (s *Server) statusSnapshot() proxy.StatusSnapshot {
	cur := s.Config()
	mgmt := s.managementServer()
	pprofEnabled := mgmt != nil && mgmt.PprofEnabled()
	if cur == nil {
		return proxy.StatusSnapshot{Status: "up", PprofEnabled: pprofEnabled}
	}
	listens := []string{}
	for _, lc := range cur.Listeners {
		listens = append(listens, lc.Listen)
	}
	mgr := s.Manager()
	ready := s.Router() != nil && mgr != nil
	if ready {
		// 如果有上游配置但没有一个 alive，则 readiness 降级。
		if !mgr.HasAliveUpstreams() && len(cur.Upstreams) > 0 {
			ready = false
		}
	}
	return proxy.StatusSnapshot{
		Status:        "up",
		Listen:        strings.Join(listens, ","),
		Management:    cur.Management,
		UpstreamCount: len(cur.Upstreams),
		RouterReady:   s.Router() != nil,
		ManagerReady:  mgr != nil,
		PprofEnabled:  pprofEnabled,
		Ready:         ready,
		Upstreams:     mgr.UpstreamStatuses(),
	}
}

// reloadConfig 从磁盘重新加载并应用配置，供管理 API 使用。
func (s *Server) reloadConfig() error {
	return s.LoadAndApply()
}

func (s *Server) currentRuntime() *runtimeState {
	return s.runtime.Load()
}

// Config 返回当前生效的配置
func (s *Server) Config() *Config {
	if state := s.currentRuntime(); state != nil {
		return state.config
	}
	return nil
}

// Manager 返回当前生效的上游管理器
func (s *Server) Manager() *proxy.UpstreamManager {
	if state := s.currentRuntime(); state != nil {
		return state.manager
	}
	return nil
}

// Router 返回当前生效的路由器
func (s *Server) Router() *proxy.Router {
	if state := s.currentRuntime(); state != nil {
		return state.router
	}
	return nil
}

// ApplyConfig 应用新配置，替换上游管理器并重启健康检查
func (s *Server) ApplyConfig(cfg *Config) error {
	return s.applyConfig(cfg, true)
}

func (s *Server) applyConfig(cfg *Config, initialLoad bool) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	select {
	case <-s.quit:
		return errors.New("server is stopping")
	default:
	}

	// 确保无论是旧版单 listen 还是直接构造的 Config，listeners 都被归一化。
	baseDir := filepath.Dir(s.cfgPath)
	if err := normalizeListeners(cfg, baseDir); err != nil {
		return err
	}

	mgr, err := proxy.NewUpstreamManager(cfg.Upstreams)
	if err != nil {
		return err
	}
	// 热加载时从旧 manager 复制未变化节点的运行时状态（健康、退避、能力等）。
	if oldState := s.currentRuntime(); oldState != nil && oldState.manager != nil {
		mgr.MergeRuntimeState(oldState.manager)
	}
	listenerRuntimes := make(map[string]*listenerRuntime, len(cfg.Listeners))
	for i, lc := range cfg.Listeners {
		access, err := proxy.NewAccessController(lc.AccessControl)
		if err != nil {
			return fmt.Errorf("listeners[%d].access_control: %w", i, err)
		}
		listenerRuntimes[lc.Listen] = &listenerRuntime{
			config: lc,
			access: access,
		}
	}
	updater := proxy.NewGeoSiteUpdater(cfg.Routing.GeoSite, mgr)
	if initialLoad && proxy.HasGeoSiteRules(cfg.Routing.Rules) && cfg.Routing.GeoSite.Path != "" {
		if !updater.IsFileAvailable() {
			proxy.Logger().Warnw("geosite dat not found or invalid, geosite rules disabled; will retry periodically",
				"path", cfg.Routing.GeoSite.Path)
			updater.StartMissingFileRecovery(func() {
				loaded, err := LoadConfig(s.cfgPath)
				if err != nil {
					proxy.Logger().Warnw("load config after geosite recovery failed", "error", err)
					return
				}
				if err := s.applyConfig(loaded, false); err != nil {
					proxy.Logger().Warnw("reload config after geosite recovery failed", "error", err)
				}
			})
		}
	}
	policy, err := proxy.NewDestinationPolicy(cfg.DestinationPolicy)
	if err != nil {
		return err
	}
	cfg.Routing.Policy = policy
	router, err := proxy.NewRouter(cfg.Routing, cfg.DNS)
	if err != nil {
		return err
	}
	if err := proxy.InitLogger(cfg.Log); err != nil {
		return err
	}
	router.Start()

	trafficAccounter := proxy.NewTrafficAccounter(cfg.TrafficAudit)
	proxy.SetGlobalTrafficAccounter(trafficAccounter)

	oldState := s.currentRuntime()
	newState := &runtimeState{
		config:           cfg,
		manager:          mgr,
		router:           router,
		geoUpdate:        updater,
		listeners:        listenerRuntimes,
		trafficAccounter: trafficAccounter,
	}

	s.stateMu.RLock()
	oldMgmt := s.mgmt
	oldMgmtAddr := s.mgmtAddr
	s.stateMu.RUnlock()
	mgmtChanged := oldMgmt == nil || oldMgmtAddr != cfg.Management || oldMgmt.PprofEnabled() != cfg.EnablePprof || oldMgmt.AuthToken() != cfg.ManagementAuthToken
	newMgmt := oldMgmt
	if mgmtChanged {
		newMgmt = s.newManagementServer(cfg)
		if cfg.Management != "" {
			mgmtLn, err := net.Listen("tcp", cfg.Management)
			if err != nil {
				return fmt.Errorf("prepare management listener on %s: %w", cfg.Management, err)
			}
			if err := newMgmt.StartListener(mgmtLn); err != nil {
				_ = mgmtLn.Close()
				return fmt.Errorf("start management server on %s: %w", cfg.Management, err)
			}
		}
	}

	newListeners := make(map[string]net.Listener)
	if !initialLoad {
		for _, lc := range cfg.Listeners {
			oldLn := s.currentListener(lc.Listen)
			oldLc := oldListenerConfig(oldState, lc.Listen)
			if oldLn != nil && oldLc != nil && oldLc.Listen == lc.Listen && oldLc.TLS == lc.TLS && oldLc.TProxy == lc.TProxy {
				newListeners[lc.Listen] = oldLn
				continue
			}
			ln, err := createListener(lc.Listen, lc.TLS, lc.TProxy)
			if err != nil {
				for _, l := range newListeners {
					_ = l.Close()
				}
				if mgmtChanged && newMgmt != nil {
					newMgmt.Stop()
				}
				return fmt.Errorf("prepare listener on %s: %w", lc.Listen, err)
			}
			newListeners[lc.Listen] = ln
		}
	}

	proxy.SetTimeouts(cfg.Timeouts)
	s.runtime.Store(newState)

	if !initialLoad {
		s.listenersMu.Lock()
		oldListeners := s.listeners
		s.listeners = newListeners
		s.listenersMu.Unlock()
		for addr, oldLn := range oldListeners {
			if newLn, ok := newListeners[addr]; ok && newLn == oldLn {
				continue
			}
			// 关闭旧 listener 停止接收新连接，已建立的连接通过空闲超时自然排空。
			_ = oldLn.Close()
			proxy.Logger().Infow("listener replaced, existing connections will drain", "address", addr)
		}
		for addr, newLn := range newListeners {
			if oldLn, ok := oldListeners[addr]; ok && oldLn == newLn {
				continue
			}
			go s.serveListener(addr, newLn)
		}
	}
	if mgmtChanged {
		s.stateMu.Lock()
		s.mgmt = newMgmt
		s.mgmtAddr = cfg.Management
		s.stateMu.Unlock()
	}

	mgr.StartHealthCheck(cfg.CheckInterval)
	for _, lr := range listenerRuntimes {
		if lr.access != nil {
			lr.access.StartCleanup(5*time.Minute, 10*time.Minute)
		}
	}
	updater.Start(func() {
		loaded, err := LoadConfig(s.cfgPath)
		if err != nil {
			proxy.Logger().Warnw("load config after geosite update failed", "error", err)
			return
		}
		if err := s.applyConfig(loaded, false); err != nil {
			proxy.Logger().Warnw("reload config after geosite update failed", "error", err)
		}
	})

	if oldState != nil {
		if oldState.geoUpdate != nil {
			oldState.geoUpdate.Stop()
		}
		if oldState.manager != nil {
			oldState.manager.StopHealthCheck()
		}
		if oldState.router != nil {
			oldState.router.Stop()
		}
		if oldState.trafficAccounter != nil {
			oldState.trafficAccounter.Stop()
		}
		for _, lr := range oldState.listeners {
			if lr.access != nil {
				lr.access.StopCleanup()
			}
		}
	}
	if mgmtChanged && oldMgmt != nil {
		oldMgmt.Stop()
	}
	return nil
}

func oldListenerConfig(state *runtimeState, addr string) *ListenerConfig {
	if state == nil || state.listeners == nil {
		return nil
	}
	lr, ok := state.listeners[addr]
	if !ok {
		return nil
	}
	return &lr.config
}

func (s *Server) currentListener(addr string) net.Listener {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	return s.listeners[addr]
}

func (s *Server) newManagementServer(cfg *Config) *proxy.ManagementServer {
	listenAddrs := make([]string, 0, len(cfg.Listeners))
	for _, lc := range cfg.Listeners {
		listenAddrs = append(listenAddrs, lc.Listen)
	}
	listen := cfg.Listen
	if len(listenAddrs) > 0 {
		listen = strings.Join(listenAddrs, ",")
	}
	mgmt := proxy.NewManagementServer(func() proxy.HealthStatus {
		curCfg := s.Config()
		if curCfg == nil {
			return proxy.HealthStatus{Status: "up"}
		}
		return proxy.HealthStatus{
			Status:        "up",
			Listen:        listen,
			UpstreamCount: len(curCfg.Upstreams),
			ManagerReady:  s.Manager() != nil,
			RouterReady:   s.Router() != nil,
		}
	}, cfg.EnablePprof, cfg.ManagementAuthToken)
	mgmt.SetStatusSnapshotGetter(s.statusSnapshot)
	mgmt.SetReload(s.reloadConfig)
	mgmt.SetConfigEditor(newConfigEditor(s.cfgPath, s.reloadConfig))
	return mgmt
}

// LoadAndApply 从磁盘重新加载并应用配置
func (s *Server) LoadAndApply() error {
	cfg, err := LoadConfig(s.cfgPath)
	if err != nil {
		return err
	}
	return s.applyConfig(cfg, false)
}

// Start 启动所有监听并处理连接。
func (s *Server) Start() error {
	cfg := s.Config()
	if cfg == nil {
		return errors.New("server has no config")
	}

	listeners := make(map[string]net.Listener)
	for _, lc := range cfg.Listeners {
		ln, err := createListener(lc.Listen, lc.TLS, lc.TProxy)
		if err != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			return err
		}
		listeners[lc.Listen] = ln
		proxy.Logger().Infow("yigate listening", "address", lc.Listen, "tls", lc.TLS.CertFile != "", "tproxy", lc.TProxy.Enabled)
	}

	s.listenersMu.Lock()
	s.listeners = listeners
	s.listenersMu.Unlock()

	if err := s.watchConfig(); err != nil {
		proxy.Logger().Warnw("watch config failed", "error", err)
	}

	for addr, ln := range listeners {
		go s.serveListener(addr, ln)
	}

	<-s.quit
	return nil
}

func (s *Server) serveListener(addr string, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				if s.currentListener(addr) != ln {
					// listener 已被替换，退出旧循环
					return
				}
				proxy.Logger().Warnw("accept error", "address", addr, "error", err)
				continue
			}
		}
		if !s.acquireConnSlot(conn, s.maxConnsForListener(addr)) {
			continue
		}
		go func() {
			defer s.releaseConnSlot(conn)
			s.handle(conn, addr)
		}()
	}
}

func (s *Server) maxConnsForListener(addr string) int64 {
	state := s.currentRuntime()
	if state == nil || state.listeners == nil {
		return 0
	}
	lr, ok := state.listeners[addr]
	if !ok {
		return 0
	}
	max := lr.config.MaxConnections
	if max <= 0 {
		max = state.config.MaxConnections
	}
	return int64(max)
}
func (s *Server) acquireConnSlot(conn net.Conn, max int64) bool {
	for {
		cur := s.connCount.Load()
		if max > 0 && cur >= max {
			proxy.Logger().Warnw("connection limit reached, closing connection", "remote", conn.RemoteAddr(), "active", cur, "max", max)
			conn.Close()
			return false
		}
		if s.connCount.CompareAndSwap(cur, cur+1) {
			s.activeMu.Lock()
			if s.stopping {
				s.activeMu.Unlock()
				s.connCount.Add(-1)
				_ = conn.Close()
				return false
			}
			s.connections[conn] = struct{}{}
			s.activeConns.Add(1)
			s.activeMu.Unlock()
			proxy.MetricSet("yigate_connections_active", float64(cur+1))
			proxy.MetricInc("yigate_connections_total")
			return true
		}
	}
}

func (s *Server) releaseConnSlot(conn net.Conn) {
	s.activeMu.Lock()
	if _, exists := s.connections[conn]; exists {
		delete(s.connections, conn)
		s.activeConns.Done()
	}
	s.activeMu.Unlock()
	for {
		cur := s.connCount.Load()
		if cur <= 0 {
			proxy.MetricSet("yigate_connections_active", 0)
			return
		}
		if s.connCount.CompareAndSwap(cur, cur-1) {
			proxy.MetricSet("yigate_connections_active", float64(cur-1))
			return
		}
	}
}

// Stop 使用默认期限优雅停止服务器。
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}

// Shutdown 在期限内等待活动连接结束，超时后主动关闭剩余连接。
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() {
		close(s.quit)
		s.activeMu.Lock()
		s.stopping = true
		s.activeMu.Unlock()
		s.listenersMu.Lock()
		for _, ln := range s.listeners {
			_ = ln.Close()
		}
		s.listeners = make(map[string]net.Listener)
		s.listenersMu.Unlock()
		s.watcherMu.Lock()
		if s.watcher != nil {
			_ = s.watcher.Close()
			s.watcher = nil
		}
		s.watcherMu.Unlock()
		s.watcherWG.Wait()

		s.applyMu.Lock()
		state := s.currentRuntime()
		s.stateMu.RLock()
		mgmt := s.mgmt
		s.stateMu.RUnlock()
		if state != nil {
			if state.geoUpdate != nil {
				state.geoUpdate.Stop()
			}
			if state.manager != nil {
				state.manager.StopHealthCheck()
			}
			if state.router != nil {
				state.router.Stop()
			}
			if state.trafficAccounter != nil {
				state.trafficAccounter.Stop()
			}
			for _, lr := range state.listeners {
				if lr.access != nil {
					lr.access.StopCleanup()
				}
			}
		}
		if mgmt != nil {
			mgmt.Stop()
		}
		s.applyMu.Unlock()

		done := make(chan struct{})
		go func() {
			s.activeConns.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			s.activeMu.Lock()
			connections := make([]net.Conn, 0, len(s.connections))
			for conn := range s.connections {
				connections = append(connections, conn)
			}
			s.activeMu.Unlock()
			for _, conn := range connections {
				_ = conn.Close()
			}
			<-done
			s.shutdownErr = ctx.Err()
		}
	})
	return s.shutdownErr
}

func (s *Server) watchConfig() error {
	s.watcherMu.Lock()
	defer s.watcherMu.Unlock()
	if s.watcher != nil {
		s.watcher.Close()
		s.watcher = nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.cfgPath)
	if err := w.Add(dir); err != nil {
		w.Close()
		return err
	}
	s.watcher = w

	s.watcherWG.Add(1)
	go func() {
		defer s.watcherWG.Done()
		s.watchLoop(w)
	}()
	return nil
}

func (s *Server) watchLoop(w *fsnotify.Watcher) {
	debounce := time.NewTimer(0)
	<-debounce.C
	pending := false
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) != filepath.Clean(s.cfgPath) {
				continue
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove) {
				if !pending {
					pending = true
					debounce = time.NewTimer(300 * time.Millisecond)
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(300 * time.Millisecond)
				}
			}
		case <-debounce.C:
			if pending {
				pending = false
				proxy.Logger().Infow("config changed, reloading")
				if err := s.LoadAndApply(); err != nil {
					proxy.Logger().Errorw("reload config failed", "error", err)
				} else {
					proxy.Logger().Infow("config reloaded", "upstream_count", len(s.Config().Upstreams))
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			proxy.Logger().Warnw("config watcher error", "error", err)
		}
	}
}

// buildTLSConfig 根据 TLS 配置构造 *tls.Config，支持 mTLS。
func buildTLSConfig(tlsCfg TLSConfig) (*tls.Config, error) {
	if strings.TrimSpace(tlsCfg.CertFile) == "" || strings.TrimSpace(tlsCfg.KeyFile) == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair: %w", err)
	}
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if strings.TrimSpace(tlsCfg.ClientCAFile) != "" {
		caPEM, err := os.ReadFile(tlsCfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse client CA certificate")
		}
		config.ClientCAs = pool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return config, nil
}

// createListener 根据 TLS 和 TPROXY 配置创建监听。
func createListener(addr string, tlsCfg TLSConfig, tproxyCfg proxy.TProxyConfig) (net.Listener, error) {
	// TPROXY 模式需要 IP_TRANSPARENT socket option
	if tproxyCfg.Enabled && tproxyCfg.NormalizedMode() == "tproxy" {
		return proxy.CreateTProxyListener(addr)
	}
	baseLn, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	config, err := buildTLSConfig(tlsCfg)
	if err != nil {
		_ = baseLn.Close()
		return nil, err
	}
	if config == nil {
		return baseLn, nil
	}
	return tls.NewListener(baseLn, config), nil
}

func (s *Server) handle(conn net.Conn, addr string) {
	state := s.currentRuntime()
	if state == nil || state.config == nil || state.manager == nil || state.router == nil {
		conn.Close()
		return
	}
	mgr := state.manager
	router := state.router

	lc := s.listenerContext(addr)
	if lc == nil {
		proxy.Logger().Warnw("no listener context", "address", addr)
		conn.Close()
		return
	}

	if lc.access != nil {
		if !lc.access.IsAllowedIP(conn.RemoteAddr()) {
			proxy.Logger().Warnw("connection denied by ACL", "remote", conn.RemoteAddr())
			conn.Close()
			return
		}
		if !lc.access.AllowConn(conn.RemoteAddr()) {
			proxy.Logger().Warnw("connection rate limited", "remote", conn.RemoteAddr())
			conn.Close()
			return
		}
	}

	// 透明代理模式：跳过协议检测，直接获取原始目标并隧道转发
	if lc.config.TProxy.Enabled {
		proxy.HandleTProxy(conn, mgr, router, lc.config.TProxy.NormalizedMode())
		return
	}

	if err := conn.SetDeadline(time.Now().Add(proxy.ProxyHandshakeTimeout())); err != nil {
		proxy.Logger().Warnw("set handshake deadline failed", "error", err)
		conn.Close()
		return
	}

	br := bufio.NewReader(conn)
	b, err := br.Peek(1)
	if err != nil {
		conn.Close()
		return
	}

	if b[0] == 0x05 {
		proxy.HandleSOCKS5(conn, br, lc.config.Users, mgr, router, lc.access, lc.config.Socks5.AllowBind, lc.config.Socks5.AllowUDPAssociate)
	} else {
		proxy.HandleHTTP(conn, br, lc.config.Users, mgr, router, lc.access)
	}
}

func (s *Server) listenerContext(addr string) *listenerContext {
	state := s.currentRuntime()
	if state == nil || state.listeners == nil {
		return nil
	}
	lr, ok := state.listeners[addr]
	if !ok {
		return nil
	}
	return &listenerContext{
		config: lr.config,
		access: lr.access,
	}
}
