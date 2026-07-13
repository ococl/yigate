package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"k5pw/proxy"
)

type runtimeState struct {
	config    *Config
	manager   *proxy.UpstreamManager
	router    *proxy.Router
	access    *proxy.AccessController
	geoUpdate *proxy.GeoSiteUpdater
}

// Server 统一代理服务器，支持配置热加载
type Server struct {
	cfgPath     string
	runtime     atomic.Pointer[runtimeState]
	mgmt        *proxy.ManagementServer
	mgmtAddr    string
	stateMu     sync.RWMutex
	applyMu     sync.Mutex
	lnMu        sync.Mutex
	ln          net.Listener
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
	return proxy.StatusSnapshot{
		Status:        "up",
		Listen:        cur.Listen,
		Management:    cur.Management,
		UpstreamCount: len(cur.Upstreams),
		RouterReady:   s.Router() != nil,
		ManagerReady:  s.Manager() != nil,
		PprofEnabled:  pprofEnabled,
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

	mgr, err := proxy.NewUpstreamManager(cfg.Upstreams)
	if err != nil {
		return err
	}
	access, err := proxy.NewAccessController(cfg.AccessControl)
	if err != nil {
		return err
	}
	updater := proxy.NewGeoSiteUpdater(cfg.Routing.GeoSite, mgr)
	if initialLoad && cfg.Routing.GeoSite.AutoUpdate && cfg.Routing.GeoSite.Path != "" {
		if _, err := os.Stat(cfg.Routing.GeoSite.Path); os.IsNotExist(err) {
			// 首次加载且文件缺失时改为后台异步下载，避免阻塞启动
			// （尤其在隔离网络或国内网络下下载可能很慢甚至失败）。
			go func() {
				if err := updater.UpdateOnce(); err != nil {
					proxy.Logger().Warnw("initial geosite update failed", "error", err)
					return
				}
				// 下载成功后重新加载配置以应用最新的 geosite 数据。
				loaded, lerr := LoadConfig(s.cfgPath)
				if lerr != nil {
					proxy.Logger().Warnw("load config after initial geosite update failed", "error", lerr)
					return
				}
				if aerr := s.applyConfig(loaded, false); aerr != nil {
					proxy.Logger().Warnw("reload config after initial geosite update failed", "error", aerr)
				}
			}()
		}
	}
	policy, err := proxy.NewDestinationPolicy(cfg.DestinationPolicy)
	if err != nil {
		return err
	}
	cfg.Routing.Policy = policy
	router, err := proxy.NewRouter(cfg.Routing)
	if err != nil {
		return err
	}
	if err := proxy.InitLogger(cfg.Log); err != nil {
		return err
	}
	// 启动路由器后台任务（路由缓存过期清理）。其对应的 Stop 由热加载与关闭流程负责。
	router.Start()

	oldState := s.currentRuntime()
	newState := &runtimeState{
		config:    cfg,
		manager:   mgr,
		router:    router,
		access:    access,
		geoUpdate: updater,
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

	var newLn net.Listener
	s.lnMu.Lock()
	currentLn := s.ln
	s.lnMu.Unlock()
	if !initialLoad && oldState != nil && oldState.config.Listen != cfg.Listen && currentLn != nil {
		newLn, err = net.Listen("tcp", cfg.Listen)
		if err != nil {
			if mgmtChanged && newMgmt != nil {
				newMgmt.Stop()
			}
			return fmt.Errorf("prepare proxy listener on %s: %w", cfg.Listen, err)
		}
	}

	proxy.SetTimeouts(cfg.Timeouts)
	s.runtime.Store(newState)
	s.maxConns.Store(int64(cfg.MaxConnections))

	if newLn != nil {
		s.lnMu.Lock()
		currentLn = s.ln
		s.ln = newLn
		s.lnMu.Unlock()
	}
	if mgmtChanged {
		s.stateMu.Lock()
		s.mgmt = newMgmt
		s.mgmtAddr = cfg.Management
		s.stateMu.Unlock()
	}

	mgr.StartHealthCheck(cfg.CheckInterval)
	access.StartCleanup(5*time.Minute, 10*time.Minute)
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

	if newLn != nil && currentLn != nil {
		_ = currentLn.Close()
	}
	if oldState != nil {
		if oldState.access != nil {
			oldState.access.StopCleanup()
		}
		if oldState.geoUpdate != nil {
			oldState.geoUpdate.Stop()
		}
		if oldState.manager != nil {
			oldState.manager.StopHealthCheck()
		}
		if oldState.router != nil {
			oldState.router.Stop()
		}
	}
	if mgmtChanged && oldMgmt != nil {
		oldMgmt.Stop()
	}
	return nil
}

func (s *Server) newManagementServer(cfg *Config) *proxy.ManagementServer {
	mgmt := proxy.NewManagementServer(func() proxy.HealthStatus {
		curCfg := s.Config()
		if curCfg == nil {
			return proxy.HealthStatus{Status: "up"}
		}
		return proxy.HealthStatus{
			Status:        "up",
			Listen:        curCfg.Listen,
			UpstreamCount: len(curCfg.Upstreams),
			ManagerReady:  s.Manager() != nil,
			RouterReady:   s.Router() != nil,
		}
	}, cfg.EnablePprof, cfg.ManagementAuthToken)
	mgmt.SetStatusSnapshotGetter(s.statusSnapshot)
	mgmt.SetReload(s.reloadConfig)
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

// Start 启动监听并处理连接
func (s *Server) Start() error {
	cfg := s.Config()
	if cfg == nil {
		return errors.New("server has no config")
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	s.lnMu.Lock()
	select {
	case <-s.quit:
		s.lnMu.Unlock()
		ln.Close()
		return nil
	default:
		s.ln = ln
	}
	s.lnMu.Unlock()
	proxy.Logger().Infow("k5pw listening", "address", cfg.Listen)

	if err := s.watchConfig(); err != nil {
		proxy.Logger().Warnw("watch config failed", "error", err)
	}

	for {
		s.lnMu.Lock()
		curLn := s.ln
		s.lnMu.Unlock()
		if curLn == nil {
			select {
			case <-s.quit:
				return nil
			default:
				proxy.Logger().Warnw("listener is nil, waiting for new listener")
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		conn, err := curLn.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return nil
			default:
				// 监听地址可能被切换，检查是否已替换 listener
				s.lnMu.Lock()
				newLn := s.ln
				s.lnMu.Unlock()
				if newLn != curLn {
					continue
				}
				proxy.Logger().Warnw("accept error", "error", err)
				continue
			}
		}
		if !s.acquireConnSlot(conn) {
			continue
		}
		go func() {
			defer s.releaseConnSlot(conn)
			s.handle(conn)
		}()
	}
}

func (s *Server) acquireConnSlot(conn net.Conn) bool {
	max := s.maxConns.Load()
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
			proxy.MetricSet("k5pw_connections_active", float64(cur+1))
			proxy.MetricInc("k5pw_connections_total")
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
			proxy.MetricSet("k5pw_connections_active", 0)
			return
		}
		if s.connCount.CompareAndSwap(cur, cur-1) {
			proxy.MetricSet("k5pw_connections_active", float64(cur-1))
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
		s.lnMu.Lock()
		if s.ln != nil {
			_ = s.ln.Close()
			s.ln = nil
		}
		s.lnMu.Unlock()
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
			if state.access != nil {
				state.access.StopCleanup()
			}
			if state.geoUpdate != nil {
				state.geoUpdate.Stop()
			}
			if state.manager != nil {
				state.manager.StopHealthCheck()
			}
			if state.router != nil {
				state.router.Stop()
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

func (s *Server) handle(conn net.Conn) {
	// 连接关闭由 public handler 负责，避免重复关闭。
	state := s.currentRuntime()
	if state == nil || state.config == nil || state.manager == nil || state.router == nil {
		conn.Close()
		return
	}
	cfg := state.config
	mgr := state.manager
	router := state.router
	access := state.access

	if access != nil {
		if !access.IsAllowedIP(conn.RemoteAddr()) {
			proxy.Logger().Warnw("connection denied by ACL", "remote", conn.RemoteAddr())
			conn.Close()
			return
		}
		if !access.AllowConn(conn.RemoteAddr()) {
			proxy.Logger().Warnw("connection rate limited", "remote", conn.RemoteAddr())
			conn.Close()
			return
		}
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
		proxy.HandleSOCKS(conn, br, cfg.Users, mgr, router, access, cfg.Socks5.AllowBind, cfg.Socks5.AllowUDPAssociate)
	} else {
		proxy.HandleHTTP(conn, br, cfg.Users, mgr, router, access)
	}
}
