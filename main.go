package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to config file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: k5pw [-config <path>] <command>\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  run       run in foreground (default)\n")
		fmt.Fprintf(os.Stderr, "  service   run as system service (used by service manager)\n")
		fmt.Fprintf(os.Stderr, "  install   install as system service\n")
		fmt.Fprintf(os.Stderr, "  uninstall uninstall system service\n")
		fmt.Fprintf(os.Stderr, "  start     start installed service\n")
		fmt.Fprintf(os.Stderr, "  stop      stop installed service\n")
		fmt.Fprintf(os.Stderr, "  restart   restart installed service\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	cmd := "run"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	cfgPath, err := filepath.Abs(*configPath)
	if err != nil {
		log.Fatalf("resolve config path: %v", err)
	}

	switch cmd {
	case "run":
		runForeground(cfgPath)
	case "service":
		runService(cfgPath)
	case "install", "uninstall", "start", "stop", "restart":
		controlService(cfgPath, cmd)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func runForeground(cfgPath string) {
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	srv, err := NewServer(cfgPath, cfg)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	log.Printf("k5pw proxy started on %s", cfg.Listen)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	srv.Stop()
	log.Println("k5pw proxy stopped")
}

func runService(cfgPath string) {
	s := newService(cfgPath)
	if err := s.Run(); err != nil {
		log.Fatalf("service run: %v", err)
	}
}

func controlService(cfgPath, action string) {
	s := newService(cfgPath)
	var err error
	switch action {
	case "install":
		err = s.Install()
	case "uninstall":
		err = s.Uninstall()
	case "start":
		err = s.Start()
	case "stop":
		err = s.Stop()
	case "restart":
		err = s.Restart()
	}
	if err != nil {
		log.Fatalf("service %s: %v", action, err)
	}
	log.Printf("service %s succeeded", action)
}
