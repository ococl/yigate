package main

import (
	"bufio"
	"log"
	"net"

	"k5pw/proxy"
)

// Server 统一代理服务器
type Server struct {
	cfg *Config
	mgr *proxy.UpstreamManager
}

// NewServer 创建服务器
func NewServer(cfg *Config) (*Server, error) {
	mgr, err := proxy.NewUpstreamManager(cfg.Upstreams)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, mgr: mgr}, nil
}

// Start 启动监听并处理连接
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Printf("k5pw listening on %s", s.cfg.Listen)
	go s.mgr.StartHealthCheck(s.cfg.CheckInterval)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	b, err := br.Peek(1)
	if err != nil {
		return
	}

	if b[0] == 0x05 {
		proxy.HandleSOCKS(conn, br, s.cfg.Users, s.mgr)
	} else {
		proxy.HandleHTTP(conn, br, s.cfg.Users, s.mgr)
	}
}
