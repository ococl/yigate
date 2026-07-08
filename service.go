package main

import (
	"log"

	"github.com/kardianos/service"
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
	srv, err := NewServer(p.cfgPath, cfg)
	if err != nil {
		return err
	}
	p.server = srv
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("server error: %v", err)
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
		log.Fatalf("create service: %v", err)
	}
	return s
}
