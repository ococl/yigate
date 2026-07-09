package main

import (
	"bufio"
	"errors"
	"log"
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
	geoUpdate   *proxy.GeoSiteUpdater
	lnMu        sync.Mutex
	ln          net.Listener
	quit        chan struct{}
	stopOnce    sync.Once
	activeConns sync.WaitGroup
	watcherMu   sync.Mutex
	watcher     *fsnotify.Watcher
	connCount   atomic.Int64
	maxConns    atomic.Int64
}

// NewServer 创建服务器，cfg 为初始配置
func NewServer(cfgPath string, cfg *Config) (*Server, error) {
	s := &Server{
		cfgPath: cfgPath,
		quit:    make(chan struct{}),
	}
	if err := s.ApplyConfig(cfg); err != nil {
		return nil, err
	}
	return s, nil
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
	mgr, err := proxy.NewUpstreamManager(cfg.Upstreams)
	if err != nil {
		return err
	}
	updater := proxy.NewGeoSiteUpdater(cfg.Routing.GeoSite, mgr)
	if initialLoad && cfg.Routing.GeoSite.AutoUpdate {
		// 仅在 geosite 文件不存在时同步下载，避免每次启动/重载都阻塞。
		if _, err := os.Stat(cfg.Routing.GeoSite.Path); os.IsNotExist(err) {
			if err := updater.UpdateOnce(); err != nil {
				log.Printf("initial geosite update failed: %v", err)
			}
		}
	}
	router, err := proxy.NewRouter(cfg.Routing)
	if err != nil {
		return err
	}
	if oldUpdater := s.geoUpdate; oldUpdater != nil {
		oldUpdater.Stop()
	}
	if old := s.Manager(); old != nil {
		old.StopHealthCheck()
	}
	mgr.StartHealthCheck(cfg.CheckInterval)
	updater.Start(func() {
		cfg, err := LoadConfig(s.cfgPath)
		if err != nil {
			log.Printf("load after geosite update: %v", err)
			return
		}
		if err := s.applyConfig(cfg, false); err != nil {
			log.Printf("reload after geosite update: %v", err)
		}
	})
	s.maxConns.Store(int64(cfg.MaxConnections))
	proxy.SetTimeouts(cfg.Timeouts)
	s.config.Store(cfg)
	s.mgr.Store(mgr)
	s.router.Store(router)
	s.geoUpdate = updater
	return nil
}

// LoadAndApply 从磁盘重新加载并应用配置
func (s *Server) LoadAndApply() error {
	cfg, err := LoadConfig(s.cfgPath)
	if err != nil {
		return err
	}
	if old := s.Config(); old != nil && old.Listen != cfg.Listen {
		log.Printf("config listen changed from %s to %s, restart required to apply", old.Listen, cfg.Listen)
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
	log.Printf("k5pw listening on %s", cfg.Listen)

	if err := s.watchConfig(); err != nil {
		log.Printf("watch config: %v", err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return nil
			default:
				log.Printf("accept error: %v", err)
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
			log.Printf("connection limit reached, closing %s", conn.RemoteAddr())
			conn.Close()
			return false
		}
		if s.connCount.CompareAndSwap(cur, cur+1) {
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
			return
		}
		if s.connCount.CompareAndSwap(cur, cur-1) {
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
		if s.geoUpdate != nil {
			s.geoUpdate.Stop()
		}
		if mgr := s.Manager(); mgr != nil {
			mgr.StopHealthCheck()
		}
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

	go s.watchLoop(w)
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
				log.Println("config changed, reloading...")
				if err := s.LoadAndApply(); err != nil {
					log.Printf("reload config: %v", err)
				} else {
					log.Printf("config reloaded: %d upstream(s)", len(s.Config().Upstreams))
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("config watcher error: %v", err)
		}
	}
}

func (s *Server) handle(conn net.Conn) {
	// 连接关闭由 public handler 负责，避免重复关闭。
	cfg := s.Config()
	mgr := s.Manager()
	router := s.Router()
	if cfg == nil || mgr == nil || router == nil {
		conn.Close()
		return
	}

	if err := conn.SetDeadline(time.Now().Add(proxy.ProxyHandshakeTimeout())); err != nil {
		log.Printf("set handshake deadline: %v", err)
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
		proxy.HandleSOCKS(conn, br, cfg.Users, mgr, router)
	} else {
		proxy.HandleHTTP(conn, br, cfg.Users, mgr, router)
	}
}
