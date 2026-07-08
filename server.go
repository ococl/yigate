package main

import (
	"bufio"
	"errors"
	"log"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"k5pw/proxy"
)

const handshakeTimeout = 15 * time.Second

// Server 统一代理服务器，支持配置热加载
type Server struct {
	cfgPath   string
	config    atomic.Value
	mgr       atomic.Value
	lnMu      sync.Mutex
	ln        net.Listener
	quit      chan struct{}
	stopOnce  sync.Once
	watcher   *fsnotify.Watcher
	connSemMu sync.RWMutex
	connSem   chan struct{}
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

// ApplyConfig 应用新配置，替换上游管理器并重启健康检查
func (s *Server) ApplyConfig(cfg *Config) error {
	mgr, err := proxy.NewUpstreamManager(cfg.Upstreams)
	if err != nil {
		return err
	}
	if old := s.Manager(); old != nil {
		old.StopHealthCheck()
	}
	mgr.StartHealthCheck(cfg.CheckInterval)
	if cfg.MaxConnections > 0 {
		s.connSemMu.Lock()
		s.connSem = make(chan struct{}, cfg.MaxConnections)
		s.connSemMu.Unlock()
	} else {
		s.connSemMu.Lock()
		s.connSem = nil
		s.connSemMu.Unlock()
	}
	s.config.Store(cfg)
	s.mgr.Store(mgr)
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
	return s.ApplyConfig(cfg)
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
		sem, ok := s.acquireConnSlot(conn)
		if !ok {
			continue
		}
		go func() {
			defer s.releaseConnSlot(sem)
			s.handle(conn)
		}()
	}
}

func (s *Server) acquireConnSlot(conn net.Conn) (chan struct{}, bool) {
	s.connSemMu.RLock()
	sem := s.connSem
	s.connSemMu.RUnlock()
	if sem == nil {
		return nil, true
	}
	select {
	case sem <- struct{}{}:
		return sem, true
	default:
		log.Printf("connection limit reached, closing %s", conn.RemoteAddr())
		conn.Close()
		return nil, false
	}
}

func (s *Server) releaseConnSlot(sem chan struct{}) {
	if sem == nil {
		return
	}
	select {
	case <-sem:
	default:
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
		if s.watcher != nil {
			s.watcher.Close()
		}
		if mgr := s.Manager(); mgr != nil {
			mgr.StopHealthCheck()
		}
	})
}

func (s *Server) watchConfig() error {
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
	defer conn.Close()

	cfg := s.Config()
	mgr := s.Manager()
	if cfg == nil || mgr == nil {
		return
	}

	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		log.Printf("set handshake deadline: %v", err)
		return
	}

	br := bufio.NewReader(conn)
	b, err := br.Peek(1)
	if err != nil {
		return
	}

	if b[0] == 0x05 {
		proxy.HandleSOCKS(conn, br, cfg.Users, mgr)
	} else {
		proxy.HandleHTTP(conn, br, cfg.Users, mgr)
	}
}
