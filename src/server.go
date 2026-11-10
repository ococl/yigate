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
	// users 只包含启用状态的用户，禁用用户不参与认证。
	users map[string]string
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
	users  map[string]string
}

// Server 统一代理服务器，支持配置热加载
type Server struct {
	cfgPath            string
	runtime            atomic.Pointer[runtimeState]
	mgmt               *proxy.ManagementServer
	mgmtAddr           string
	stateMu            sync.RWMutex
	applyMu            sync.Mutex
	listenersMu        sync.Mutex
	listeners          map[string]net.Listener
	quit               chan struct{}
	stopOnce           sync.Once
	shutdownErr        error
	activeConns        sync.WaitGroup
	activeMu           sync.Mutex
	connections        map[net.Conn]struct{}
	stopping           bool
	watcherMu          sync.Mutex
	watcher            *fsnotify.Watcher
	watcherWG          sync.WaitGroup
	connCount          atomic.Int64
	maxConns           atomic.Int64
	listenerConnCounts sync.Map // map[string]*atomic.Int64，每个监听地址独立计算并发上限
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
		Status:         "up",
		Listen:         strings.Join(listens, ","),
		Management:     cur.Management,
		UpstreamCount:  len(cur.Upstreams),
		RouterReady:    s.Router() != nil,
		ManagerReady:   mgr != nil,
		PprofEnabled:   pprofEnabled,
		WebUIEnabled:   proxy.WebUIEnabled(),
		Ready:          ready,
		Upstreams:      mgr.UpstreamStatuses(),
		ProxyEndpoints: buildProxyEndpoints(cur.Listeners),
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
			users:  lc.ActiveUsers(),
		}
	}
	updater := proxy.NewGeoSiteUpdater(cfg.Routing.GeoSite, mgr)
	if initialLoad && proxy.HasGeoSiteRules(cfg.Routing.Rules) && cfg.Routing.GeoSite.Path != "" {
		if !updater.IsFileAvailable() {
			proxy.Logger().Warnw("geosite dat not found or invalid, geosite rules disabled; will retry periodically",
				"path", cfg.Routing.GeoSite.Path)
			updater.StartMissingFileRecovery(func() {
				loaded, err := LoadConfigForReload(s.cfgPath, s.Config())
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

	oldState := s.currentRuntime()
	var trafficAccounter *proxy.TrafficAccounter
	newState := &runtimeState{
		config:    cfg,
		manager:   mgr,
		router:    router,
		geoUpdate: updater,
		listeners: listenerRuntimes,
	}

	// WebUI 开关属于热加载项（未设置时默认启用）：先更新全局状态，
	// 再参与管理端口的重建判断，保证修改 webui.enabled 后无需重启即可生效。
	webuiEnabled := cfg.WebUI == nil || cfg.WebUI.Enabled
	webuiChanged := proxy.WebUIEnabled() != webuiEnabled
	proxy.SetWebUIEnabled(webuiEnabled)

	s.stateMu.RLock()
	oldMgmt := s.mgmt
	oldMgmtAddr := s.mgmtAddr
	s.stateMu.RUnlock()
	mgmtChanged := oldMgmt == nil || oldMgmtAddr != cfg.Management || oldMgmt.PprofEnabled() != cfg.EnablePprof ||
		oldMgmt.AuthToken() != cfg.ManagementAuthToken || webuiChanged
	newMgmt := oldMgmt
	reuseMgmt := false
	if mgmtChanged {
		// 管理地址未变时不能重新 Listen（同一端口仍被旧 server 占用），
		// 改为原地重建路由表，使 enable_pprof / token / WebUI 开关可热加载。
		if cfg.Management != "" && oldMgmt != nil && oldMgmtAddr == cfg.Management && oldMgmt.Addr() != "" {
			reuseMgmt = true
		} else {
			newMgmt = s.newManagementServer(cfg)
			if cfg.Management != "" {
				mgmtLn, err := net.Listen("tcp", cfg.Management)
				if err != nil {
					router.Stop()
					return fmt.Errorf("prepare management listener on %s: %w", cfg.Management, err)
				}
				if err := newMgmt.StartListener(mgmtLn); err != nil {
					_ = mgmtLn.Close()
					router.Stop()
					return fmt.Errorf("start management server on %s: %w", cfg.Management, err)
				}
			}
		}
	}

	newListeners := make(map[string]net.Listener)
	if !initialLoad {
		// 先关闭需要替换或移除的旧 listener，释放端口，避免随后 bind 同地址时
		// 因 TIME_WAIT 或端口仍被占用而报 address already in use。
		reusedAddrs := make(map[string]bool, len(cfg.Listeners))
		for _, lc := range cfg.Listeners {
			oldLn := s.currentListener(lc.Listen)
			oldLc := oldListenerConfig(oldState, lc.Listen)
			if oldLn != nil && oldLc != nil && oldLc.Listen == lc.Listen && oldLc.TLS == lc.TLS && oldLc.TProxy == lc.TProxy {
				newListeners[lc.Listen] = oldLn
				reusedAddrs[lc.Listen] = true
				continue
			}
		}
		s.listenersMu.Lock()
		oldListeners := s.listeners
		s.listenersMu.Unlock()
		for addr, oldLn := range oldListeners {
			if reusedAddrs[addr] {
				continue
			}
			_ = oldLn.Close()
			proxy.Logger().Infow("listener closed for reconfiguration", "address", addr)
		}
		// 创建新 listener（此时被替换/移除地址的端口已释放）。
		for _, lc := range cfg.Listeners {
			if reusedAddrs[lc.Listen] {
				continue
			}
			ln, err := createListener(lc.Listen, lc.TLS, lc.TProxy)
			if err != nil {
				for _, l := range newListeners {
					_ = l.Close()
				}
				// 本次已启动的路由缓存/DNS 清理任务必须一并回滚，
				// 否则热加载失败后仍会残留后台 goroutine。
				router.Stop()
				// 复用旧管理服务时不能 Stop，它仍在为现有连接服务。
				if mgmtChanged && !reuseMgmt && newMgmt != nil {
					newMgmt.Stop()
				}
				return fmt.Errorf("prepare listener on %s: %w", lc.Listen, err)
			}
			newListeners[lc.Listen] = ln
		}
	}

	proxy.SetTimeouts(cfg.Timeouts)

	// 热加载时复用同一个流量统计器：上游增删/启停不应清空历史流量。
	if oldState != nil && oldState.trafficAccounter != nil {
		trafficAccounter = oldState.trafficAccounter
		trafficAccounter.Reconfigure(cfg.TrafficAudit)
	} else {
		storePath := ""
		if s.cfgPath != "" {
			storePath = filepath.Join(baseDir, "traffic.json")
		}
		trafficAccounter = proxy.NewTrafficAccounterWithStore(cfg.TrafficAudit, storePath)
	}
	newState.trafficAccounter = trafficAccounter
	proxy.SetGlobalTrafficAccounter(trafficAccounter)
	s.runtime.Store(newState)

	if !initialLoad {
		s.listenersMu.Lock()
		oldListeners := s.listeners
		s.listeners = newListeners
		s.listenersMu.Unlock()
		for addr, newLn := range newListeners {
			if oldLn, ok := oldListeners[addr]; ok && oldLn == newLn {
				continue
			}
			go s.serveListener(addr, newLn)
		}
	}
	if mgmtChanged {
		if reuseMgmt {
			// 所有可能失败的步骤都已完成，再原地替换路由表。
			newMgmt.Reconfigure(cfg.EnablePprof, cfg.ManagementAuthToken)
		}
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
		loaded, err := LoadConfigForReload(s.cfgPath, s.Config())
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
		if oldState.trafficAccounter != nil && oldState.trafficAccounter != trafficAccounter {
			oldState.trafficAccounter.Stop()
		}
		for _, lr := range oldState.listeners {
			if lr.access != nil {
				lr.access.StopCleanup()
			}
		}
	}
	if mgmtChanged && oldMgmt != nil && oldMgmt != newMgmt {
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
	mgmt := proxy.NewManagementServer(func() proxy.HealthStatus {
		curCfg := s.Config()
		if curCfg == nil {
			return proxy.HealthStatus{Status: "up"}
		}
		// 监听地址必须在每次请求时从当前配置读取：热加载/复用管理服务时闭包内捕获的旧值会过期。
		listen := curCfg.Listen
		if len(curCfg.Listeners) > 0 {
			addrs := make([]string, 0, len(curCfg.Listeners))
			for _, lc := range curCfg.Listeners {
				addrs = append(addrs, lc.Listen)
			}
			listen = strings.Join(addrs, ",")
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
	mgmt.SetGeoSiteAccess(geoSiteAccess{s: s})
	mgmt.SetUpstreamChecker(
		func(name string) (proxy.UpstreamCheckResult, error) {
			mgr := s.Manager()
			if mgr == nil {
				return proxy.UpstreamCheckResult{}, errors.New("upstream manager not ready")
			}
			return mgr.CheckUpstream(name)
		},
		func() []proxy.UpstreamCheckResult {
			mgr := s.Manager()
			if mgr == nil {
				return nil
			}
			return mgr.CheckAllUpstreams()
		},
	)
	return mgmt
}

// LoadAndApply 从磁盘重新加载并应用配置。
// 热加载时允许密钥类环境变量缺失（沿用当前生效值），避免管理接口保存后整体回滚。
func (s *Server) LoadAndApply() error {
	cfg, err := LoadConfigForReload(s.cfgPath, s.Config())
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
		// 并发上限按监听地址独立计算；全局计数只用于指标与优雅关闭，
		// 避免一个端口的连接挤占另一个端口的配额。
		releaseListenerSlot, ok := s.acquireListenerConnSlot(addr, s.maxConnsForListener(addr))
		if !ok {
			_ = conn.Close()
			continue
		}
		if !s.acquireConnSlot(conn, 0) {
			releaseListenerSlot()
			continue
		}
		go func() {
			defer releaseListenerSlot()
			defer s.releaseConnSlot(conn)
			s.handle(conn, addr)
		}()
	}
}

// acquireListenerConnSlot 为单个监听地址独立占用一个并发槽位，返回释放函数。
// max<=0 时不做限制。
func (s *Server) acquireListenerConnSlot(addr string, max int64) (func(), bool) {
	noop := func() {}
	if max <= 0 {
		return noop, true
	}
	value, _ := s.listenerConnCounts.LoadOrStore(addr, &atomic.Int64{})
	counter := value.(*atomic.Int64)
	for {
		cur := counter.Load()
		if cur >= max {
			proxy.Logger().Warnw("listener connection limit reached, closing connection", "address", addr, "active", cur, "max", max)
			return noop, false
		}
		if counter.CompareAndSwap(cur, cur+1) {
			return func() { counter.Add(-1) }, true
		}
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
	baseLn, err := reuseAddrListen("tcp", addr)
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

// reuseAddrListen 在各平台有独立实现：见 server_unix.go 与 server_windows.go。
// Unix 上开启 SO_REUSEADDR（及 Linux 的 SO_REUSEPORT），避免热加载时
// bind: address already in use；Windows 上直接使用标准 net.Listen。

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
		proxy.HandleSOCKS5(conn, br, lc.users, mgr, router, lc.access, lc.config.Socks5.AllowBind, lc.config.Socks5.AllowUDPAssociate)
	} else {
		proxy.HandleHTTP(conn, br, lc.users, mgr, router, lc.access)
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
		users:  lr.users,
	}
}
