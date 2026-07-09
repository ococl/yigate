package main

import (
	"fmt"
	"os"

	"github.com/kardianos/service"
	"k5pw/proxy"
)

type program struct {
	cfgPath string
	server  *Server
}

func (p *program) Start(s service.Service) error {
	cfg, err := LoadConfig(p.cfgPath)
	if err != nil {
		return err
	}
	if err := proxy.InitLogger(cfg.Log); err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		return err
	}
	srv, err := NewServer(p.cfgPath, cfg)
	if err != nil {
		proxy.Logger().Errorw("create server failed", "error", err)
		return err
	}
	p.server = srv
	go func() {
		if err := srv.Start(); err != nil {
			proxy.Logger().Errorw("server error", "error", err)
		}
	}()
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.server != nil {
		p.server.Stop()
	}
	return nil
}

func newService(cfgPath string) service.Service {
	prg := &program{cfgPath: cfgPath}
	svcConfig := &service.Config{
		Name:        "k5pw",
		DisplayName: "k5pw proxy",
		Description: "SOCKS5/HTTP proxy server",
		Arguments:   []string{"-config", cfgPath, "service"},
	}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create service: %v\n", err)
		os.Exit(1)
	}
	return s
}
