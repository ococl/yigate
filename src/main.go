package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kardianos/service"
	"yigate/proxy"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "data/config.yml", "path to config file")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: yigate [-version] [-config <path>] <command>\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  run       run in foreground (default)\n")
		fmt.Fprintf(os.Stderr, "  service   run as system service (used by service manager)\n")
		fmt.Fprintf(os.Stderr, "  install   install as system service\n")
		fmt.Fprintf(os.Stderr, "  uninstall uninstall system service\n")
		fmt.Fprintf(os.Stderr, "  start     start installed service\n")
		fmt.Fprintf(os.Stderr, "  stop      stop installed service\n")
		fmt.Fprintf(os.Stderr, "  restart   restart installed service\n")
		fmt.Fprintf(os.Stderr, "  status    show installed service status\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("yigate version %s (commit %s, built %s)\n", version, commit, buildTime)
		return
	}

	// 注入版本信息，供 WebUI/管理接口展示。
	proxy.SetBuildInfo(version, commit, buildTime)

	cmd := "run"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	cfgPath, err := filepath.Abs(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve config path: %v\n", err)
		os.Exit(1)
	}

	switch cmd {
	case "run":
		runForeground(cfgPath)
	case "service":
		runService(cfgPath)
	case "install", "uninstall", "start", "stop", "restart", "status":
		controlService(cfgPath, cmd)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func runForeground(cfgPath string) {
	// 配置文件不存在时自动生成默认配置（同目录），方便首次启动。
	// 首次生成会注入一个随机 management_auth_token，仅本次在日志中提示，之后请从配置文件读取。
	if generated, token, err := WriteDefaultConfigIfMissing(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "init default config: %v\n", err)
		os.Exit(1)
	} else if generated {
		fmt.Fprintf(os.Stderr, "config not found, generated default config at %s\n", cfgPath)
		if token != "" {
			fmt.Fprintf(os.Stderr, "======================================================================\n")
			fmt.Fprintf(os.Stderr, " 首次启动已生成 WebUI 登录密码（management_auth_token）：\n")
			fmt.Fprintf(os.Stderr, "   %s\n", token)
			fmt.Fprintf(os.Stderr, " 该密码已写入配置文件，请妥善保存；重启后请从配置文件读取，日志将不再显示。\n")
			fmt.Fprintf(os.Stderr, "======================================================================\n")
		}
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	if err := proxy.InitLogger(cfg.Log); err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	srv, err := NewServer(cfgPath, cfg)
	if err != nil {
		proxy.Logger().Fatalw("create server failed", "error", err)
	}
	go func() {
		if err := srv.Start(); err != nil {
			proxy.Logger().Fatalw("server error", "error", err)
		}
	}()

	proxy.Logger().Infow("yigate proxy started", "listen", cfg.Listen)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	srv.Stop()
	proxy.Logger().Infow("yigate proxy stopped")
	proxy.CloseLogger()
}

func runService(cfgPath string) {
	if generated, token, err := WriteDefaultConfigIfMissing(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "init default config: %v\n", err)
		os.Exit(1)
	} else if generated {
		fmt.Fprintf(os.Stderr, "config not found, generated default config at %s\n", cfgPath)
		if token != "" {
			fmt.Fprintf(os.Stderr, "======================================================================\n")
			fmt.Fprintf(os.Stderr, " 首次启动已生成 WebUI 登录密码（management_auth_token）：\n")
			fmt.Fprintf(os.Stderr, "   %s\n", token)
			fmt.Fprintf(os.Stderr, " 该密码已写入配置文件，请妥善保存；重启后请从配置文件读取，日志将不再显示。\n")
			fmt.Fprintf(os.Stderr, "======================================================================\n")
		}
	}
	s := newService(cfgPath)
	if err := s.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service run: %v\n", err)
		os.Exit(1)
	}
}

func controlService(cfgPath, action string) {
	s := newService(cfgPath)
	if action == "status" {
		st, err := s.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "query service status: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(serviceStatusString(st))
		return
	}
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
		fmt.Fprintf(os.Stderr, "service %s: %v\n", action, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "service %s succeeded\n", action)
}

func serviceStatusString(st service.Status) string {
	switch st {
	case service.StatusRunning:
		return "yigate service: running"
	case service.StatusStopped:
		return "yigate service: stopped"
	case service.StatusUnknown:
		return "yigate service: unknown (not installed or status unavailable)"
	default:
		return fmt.Sprintf("yigate service: status=%d", int(st))
	}
}
