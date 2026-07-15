package main

import (
	"fmt"
	"os"
	"runtime"

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

// newService 构造跨平台系统服务封装。除基础元信息外，会按平台注入
// 重启/恢复策略（platformServiceOptions），使 k5pw 在进程异常退出或宿主
// 重启后能够自动恢复，从而支持在各平台稳定部署为系统服务。
func newService(cfgPath string) service.Service {
	prg := &program{cfgPath: cfgPath}

	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}

	svcConfig := &service.Config{
		Name:        "k5pw",
		DisplayName: "k5pw Unified Proxy",
		Description: "Lightweight unified proxy (HTTP/HTTPS CONNECT/SOCKS5) with routing, multi-upstream and management API",
		Executable:  exePath,
		Arguments:   []string{"-config", cfgPath, "service"},
		Option:      platformServiceOptions(),
	}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create service: %v\n", err)
		os.Exit(1)
	}
	return s
}

// platformServiceOptions 返回各平台系统服务管理器的平台特定选项。
// 目标一致：进程崩溃或系统重启后自动恢复，提升跨平台部署健壮性。
//   - windows：服务失败时延迟重启，并启用延迟自动启动
//   - darwin ：launchd KeepAlive，进程退出即重启
//   - 其它（linux / systemd）：Restart=always，10s 后重启，放宽启动失败计数
//
// 注意：kardianos 的 systemd 生成器仅通过 Restart / SuccessExitStatus 两个
// 选项键注入；RestartSec 与 StartLimitBurst 在默认模板中写死（分别为 120 / 10）。
// 为使 RestartSec=10 与 StartLimitBurst=64 真正生效，Linux 平台改用自定义
// SystemdScript 模板（沿用官方默认结构，仅调整这两项，并保留动态变量）。
func platformServiceOptions() map[string]interface{} {
	switch runtime.GOOS {
	case "windows":
		return map[string]interface{}{
			"OnFailure":            "restart",
			"OnFailureDelay":       "10s",
			"OnFailureResetPeriod": "3600",
			"DelayedAutoStart":     true,
		}
	case "darwin":
		return map[string]interface{}{
			"KeepAlive": true,
		}
	default:
		return map[string]interface{}{
			"Restart":       "always",
			"SystemdScript": systemdUnitTemplate,
		}
	}
}

// systemdUnitTemplate 是 Linux systemd 单元模板（传入 kardianos 的 SystemdScript）。
// 与官方默认模板一致，仅将写死的 RestartSec=120 / StartLimitBurst=10 调整为
// RestartSec=10 / StartLimitBurst=64，并保留 Restart / SuccessExitStatus 动态注入。
const systemdUnitTemplate = `[Unit]
Description={{.Description}}
ConditionFileIsExecutable={{.Path|cmdEscape}}
{{range $i, $dep := .Dependencies}}
{{$dep}} {{end}}

[Service]
StartLimitInterval=5
StartLimitBurst=64
ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmd}}{{end}}
{{if .ChRoot}}RootDirectory={{.ChRoot|cmd}}{{end}}
{{if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory|cmdEscape}}{{end}}
{{if .UserName}}User={{.UserName}}{{end}}
{{if .ReloadSignal}}ExecReload=/bin/kill -{{.ReloadSignal}} "$MAINPID"{{end}}
{{if .PIDFile}}PIDFile={{.PIDFile|cmd}}{{end}}
{{if and .LogOutput .HasOutputFileSupport -}}
StandardOutput=file:{{.LogDirectory}}/{{.Name}}.out
StandardError=file:{{.LogDirectory}}/{{.Name}}.err
{{- end}}
{{if gt .LimitNOFILE -1 }}LimitNOFILE={{.LimitNOFILE}}{{end}}
{{if .Restart}}Restart={{.Restart}}{{end}}
{{if .SuccessExitStatus}}SuccessExitStatus={{.SuccessExitStatus}}{{end}}
RestartSec=10
EnvironmentFile=-/etc/sysconfig/{{.Name}}

{{range $k, $v := .EnvVars -}}
Environment={{$k}}={{$v}}
{{end -}}

[Install]
WantedBy=multi-user.target
`
