# AGENTS.md

## 项目概述

`k5pw` 是一个 Go 语言实现的轻量级统一代理服务。核心能力是让 HTTP、HTTPS CONNECT 和 SOCKS5 共用单端口接入，并通过规则路由、多上游、健康检查、socks5h 远端解析、配置热加载和管理接口提供可运维的代理能力。

## 工作约定

- 所有修改在当前工作区原地完成，不使用 WorkTree。
- 优先保持改动小而聚焦，避免无关重构。
- 修改代码后优先运行相关 Go 测试。
- 下载 Go 依赖时优先使用国内镜像：`GOPROXY=https://goproxy.cn,direct`。
- 不要提交真实代理密码、管理 Token、上游账号或私有地址。
- `.codebuddy/` 是项目相关数据目录，不要删除。

## 技术栈

- Go 1.21
- `fsnotify`：配置文件热加载。
- `kardianos/service`：系统服务管理。
- `zap`：结构化日志。
- `lumberjack`：按大小滚动日志。
- `yaml.v3`：YAML 配置解析。

## 重要目录

```text
.
├── src/                 # Go 模块根目录
│   ├── go.mod
│   ├── main.go          # CLI 入口、版本信息、服务命令
│   ├── config.go        # YAML 配置加载、环境变量展开、校验
│   ├── server.go        # 统一 TCP Server、协议识别、配置热加载
│   ├── service.go       # system service 封装
│   ├── config.sample.yml
│   ├── Dockerfile
│   └── proxy/           # 代理核心
├── dist/                # Docker Compose、systemd、运行目录示例
├── scripts/             # 发布构建脚本
└── pyscripts/           # 联调脚本
```

## 核心模块

- `src/main.go`：解析 `-config`、`-version` 和 `run/service/install/uninstall/start/stop/restart` 命令。
- `src/config.go`：加载配置、展开环境变量、补默认值、校验监听地址和上游引用。
- `src/server.go`：监听 TCP、识别协议、应用配置、管理连接计数、监听配置变更。
- `src/service.go`：通过 `kardianos/service` 支持系统服务安装与运行。
- `src/proxy/http.go`：处理 HTTP 请求和 HTTPS CONNECT 隧道。
- `src/proxy/socks.go`：处理 SOCKS5 协商和命令分发。
- `src/proxy/socks_bind.go`：实现 SOCKS5 BIND 和 UDP ASSOCIATE。
- `src/proxy/socks_sniff.go`：通过 TLS SNI / HTTP Host 从首包恢复域名。
- `src/proxy/router.go`：编译与匹配路由规则，执行 direct/proxy/auto 决策。
- `src/proxy/upstream.go`：管理上游代理、优先级、权重和选择逻辑。
- `src/proxy/upstream_health.go`：执行上游健康检查。
- `src/proxy/management.go`：提供健康检查、指标、状态和手动 reload 接口。
- `src/proxy/metrics.go`：维护 Prometheus 文本格式指标。
- `src/proxy/logging.go`：初始化结构化日志和日志滚动。
- `src/proxy/access.go`：客户端 CIDR 白名单、单 IP 限速和用户并发限制。

## 构建与运行

```bash
cd src
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o k5pw .
./k5pw -config config.sample.yml run
```

Docker：

```bash
cd dist
docker compose up --build
```

发布构建：

```bash
./scripts/release.sh 1.2.0
```

## 测试

```bash
cd src
GOPROXY=https://goproxy.cn,direct go test ./...
```

如果只改 `src/proxy`，仍建议至少运行 `go test ./proxy`。改动配置、启动、热加载或管理接口时，运行 `go test ./...`。

## 配置要点

- 示例配置：`src/config.sample.yml`。
- `listen` 是代理端口。
- `management` 是独立管理端口。
- `management_auth_token` 非空时所有管理接口都要求 Bearer Token。
- 管理端口绑定非回环地址时必须配置 `management_auth_token`。
- `users` 为空时允许匿名代理访问。
- `upstreams[].types` 支持 `http`、`https`、`socks5`、`socks5h`。
- `routing.rules` 按顺序匹配，首个命中规则生效。
- `routing.final` 支持 `direct`、`proxy`、`auto`。
- `geosite.dat` 相对路径按配置文件所在目录解析。
- 配置中的 `${ENV_NAME}` 会被严格展开，缺失或空值会导致启动失败。

## 路由规则

支持的规则类型：

- `domain`
- `domain_suffix`
- `domain_keyword`
- `domain_regex`
- `ip_cidr`
- `geosite`
- `match`

支持的动作：

- `direct`：直连目标。
- `proxy`：通过上游代理连接。
- `auto`：按策略先试直连或代理，并缓存成功结果。

## 管理接口

- `GET /health`：返回健康状态。
- `GET /metrics`：返回 Prometheus 指标。
- `GET /api/v1/status`：返回运行摘要。
- `POST /api/v1/reload`：手动重新加载配置。
- `/debug/pprof/*`：仅在 `enable_pprof: true` 时启用。

## 开发注意事项

- 协议入口改动通常需要覆盖 HTTP、CONNECT、SOCKS5 三类路径。
- 路由逻辑改动应补充或更新 `router`、`route_cache`、`socks5h` 相关测试。
- 上游选择逻辑改动应关注优先级、权重、dead 标记、健康检查和指定上游白名单。
- 热加载逻辑改动应确认旧组件停止、新组件原子替换、监听地址切换和管理端口切换。
- 日志初始化可在热加载时发生，避免关闭仍被并发写入的 writer。
- BIND 与 UDP ASSOCIATE 只适合 SOCKS5 上游，HTTP/HTTPS 上游不支持。
- pprof 和管理接口属于敏感入口，不应默认暴露到公网。
