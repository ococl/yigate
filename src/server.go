package main

import (
	"bufio"
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

// Server 统一代理服务器，支持配置热加载
type Server struct {
	cfgPath     string
	config      atomic.Value
	mgr         atomic.Value
	router      atomic.Value
	access      *proxy.AccessController
	geoUpdate   *proxy.GeoSiteUpdater
	mgmt        *proxy.ManagementServer
	mgmtAddr    string
	stateMu     sync.RWMutex
	applyMu     sync.Mutex
	lnMu        sync.Mutex
	ln          net.Listener
	quit        chan struct{}
	stopOnce    sync.Once
	activeConns sync.WaitGroup
	watcherMu   sync.Mutex
	watcher     *fsnotify.Watcher
	watcherWG   sync.WaitGroup
	connCount   atomic.Int64
	maxConns    atomic.Int64
}

// NewServer 创建服务器，cfg 为初始配置
func NewServer(cfgPath string, cfg *Config) (*Server, error) {
	s := &Server{
		cfgPath: cfgPath,
		quit:    make(chan struct{}),
		mgmt:    proxy.NewManagementServer(nil, cfg.EnablePprof, cfg.ManagementAuthToken),
	}
	s.mgmt.SetStatusSnapshotGetter(s.statusSnapshot)
	s.mgmt.SetReload(s.reloadConfig)
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

// Config 返回当前生效的配置
func (s *Server) Config() *Config {
	if v := s.config.Load(); v != nil {
		return v.(*Config)
	}
	return nil
}

// Manager 返回当前生效的上游管理器
func (s *Server) Manager() *proxy.UpstreamManager {
	if v := s.mgr.Load(); v != nil {
		return v.(*proxy.UpstreamManager)
	}
	return nil
}

// Router 返回当前生效的路由器
func (s *Server) Router() *proxy.Router {
	if v := s.router.Load(); v != nil {
		return v.(*proxy.Router)
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
		// 仅在 geosite 文件不存在时同步下载，避免每次启动/重载都阻塞。
		if _, err := os.Stat(cfg.Routing.GeoSite.Path); os.IsNotExist(err) {
			if err := updater.UpdateOnce(); err != nil {
				proxy.Logger().Warnw("initial geosite update failed", "error", err)
			}
		}
	}
	router, err := proxy.NewRouter(cfg.Routing)
	if err != nil {
		return err
	}

	// 在替换全局引用前，先准备好日志和超时配置。
	if err := proxy.InitLogger(cfg.Log); err != nil {
		return err
	}
	s.maxConns.Store(int64(cfg.MaxConnections))
	proxy.SetTimeouts(cfg.Timeouts)

	// 记录旧组件，用于替换后优雅停止。
	s.stateMu.RLock()
	oldAccess := s.access
	oldUpdater := s.geoUpdate
	s.stateMu.RUnlock()
	oldMgr := s.Manager()
	oldRouter := s.Router()

	// 原子替换：新连接立刻看到完整的新运行时上下文。
	s.stateMu.Lock()
	s.config.Store(cfg)
	s.mgr.Store(mgr)
	s.router.Store(router)
	s.access = access
	s.geoUpdate = updater
	s.stateMu.Unlock()

	// 启动新组件的健康检查与 geosite 自动更新。
	mgr.StartHealthCheck(cfg.CheckInterval)
	updater.Start(func() {
		cfg, err := LoadConfig(s.cfgPath)
		if err != nil {
			proxy.Logger().Warnw("load config after geosite update failed", "error", err)
			return
		}
		if err := s.applyConfig(cfg, false); err != nil {
			proxy.Logger().Warnw("reload config after geosite update failed", "error", err)
		}
	})

	// 停止旧组件，避免旧 manager 的健康检查继续占用资源。
	if oldAccess != nil {
		oldAccess.StopCleanup()
	}
	if oldUpdater != nil {
		oldUpdater.Stop()
	}
	if oldMgr != nil {
		oldMgr.StopHealthCheck()
	}
	_ = oldRouter // 旧 router 无背景任务，直接由 GC 回收即可。

	s.stateMu.Lock()
	if s.mgmtAddr != cfg.Management || s.mgmt.PprofEnabled() != cfg.EnablePprof || s.mgmt.AuthToken() != cfg.ManagementAuthToken {
		if s.mgmtAddr != "" {
			s.mgmt.Stop()
		}
		// 重新创建管理服务器以切换 pprof 路由
		s.mgmt = proxy.NewManagementServer(func() proxy.HealthStatus {
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
		if cfg.Management != "" {
			if err := s.mgmt.Start(cfg.Management); err != nil {
				s.stateMu.Unlock()
				return fmt.Errorf("start management server on %s: %w", cfg.Management, err)
			}
		}
		s.mgmtAddr = cfg.Management
	} else {
		s.mgmt.SetStatusGetter(func() proxy.HealthStatus {
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
		})
	}
	s.mgmt.SetStatusSnapshotGetter(s.statusSnapshot)
	s.mgmt.SetReload(s.reloadConfig)
	s.stateMu.Unlock()

	// 启动访问控制器的周期清理，防止 rateLimiters 内存无限增长。
	if s.access != nil {
		s.access.StartCleanup(5*time.Minute, 10*time.Minute)
	}
	return nil
}

// LoadAndApply 从磁盘重新加载并应用配置
func (s *Server) LoadAndApply() error {
	cfg, err := LoadConfig(s.cfgPath)
	if err != nil {
		return err
	}

	var newLn net.Listener
	old := s.Config()
	if old != nil && old.Listen != cfg.Listen {
		proxy.Logger().Infow("config listen changed, preparing listener", "old", old.Listen, "new", cfg.Listen)
		newLn, err = net.Listen("tcp", cfg.Listen)
		if err != nil {
			return fmt.Errorf("switch listener to %s: %w", cfg.Listen, err)
		}
	}

	if err := s.applyConfig(cfg, false); err != nil {
		if newLn != nil {
			_ = newLn.Close()
		}
		return err
	}
	if newLn == nil {
		return nil
	}

	s.lnMu.Lock()
	oldLn := s.ln
	s.ln = newLn
	s.lnMu.Unlock()
	if oldLn != nil {
		_ = oldLn.Close()
	}
	return nil
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
		s.activeConns.Add(1)
		go func() {
			defer s.activeConns.Done()
			defer s.releaseConnSlot()
			s.handle(conn)
		}()
	}
}

func (s *Server) acquireConnSlot(conn net.Conn) bool {
	max := s.maxConns.Load()
	if max <= 0 {
		return true
	}
	for {
		cur := s.connCount.Load()
		if cur >= max {
			proxy.Logger().Warnw("connection limit reached, closing connection", "remote", conn.RemoteAddr(), "active", cur, "max", max)
			conn.Close()
			return false
		}
		if s.connCount.CompareAndSwap(cur, cur+1) {
			proxy.MetricSet("k5pw_connections_active", float64(cur+1))
			proxy.MetricInc("k5pw_connections_total")
			return true
		}
	}
}

func (s *Server) releaseConnSlot() {
	max := s.maxConns.Load()
	if max <= 0 {
		return
	}
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

// Stop 优雅停止服务器
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.quit)
		s.lnMu.Lock()
		if s.ln != nil {
			s.ln.Close()
		}
		s.lnMu.Unlock()
		s.watcherMu.Lock()
		if s.watcher != nil {
			s.watcher.Close()
			s.watcher = nil
		}
		s.watcherMu.Unlock()
		s.watcherWG.Wait()

		s.applyMu.Lock()
		s.stateMu.RLock()
		access := s.access
		updater := s.geoUpdate
		mgmt := s.mgmt
		s.stateMu.RUnlock()
		if access != nil {
			access.StopCleanup()
		}
		if updater != nil {
			updater.Stop()
		}
		if mgr := s.Manager(); mgr != nil {
			mgr.StopHealthCheck()
		}
		if mgmt != nil {
			mgmt.Stop()
		}
		s.applyMu.Unlock()
		s.activeConns.Wait()
	})
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
	cfg := s.Config()
	mgr := s.Manager()
	router := s.Router()
	s.stateMu.RLock()
	access := s.access
	s.stateMu.RUnlock()
	if cfg == nil || mgr == nil || router == nil {
		conn.Close()
		return
	}

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
		proxy.HandleSOCKS(conn, br, cfg.Users, mgr, router, access)
	} else {
		proxy.HandleHTTP(conn, br, cfg.Users, mgr, router, access)
	}
}
