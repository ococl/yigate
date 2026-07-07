package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	srv, err := NewServer(cfg)
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
	log.Println("k5pw proxy stopped")
}
