# AGENTS.md

## 项目概述

`k5pw` 是一个 Go 语言实现的轻量级统一代理服务。核心能力是让 HTTP、HTTPS CONNECT 和 SOCKS5 共用单端口接入，并通过规则路由、多上游、健康检查、socks5h 远端解析、配置热加载和管理接口提供可运维的代理能力。项目定位为合规矩形、轻量可运维的统一代理网关，明确不支持任何混淆/非合规协议。

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
│   ├── config_api_impl.go # 运行时配置 API 实现（持久化到 YAML）
│   ├── server.go        # 统一 TCP Server、协议识别、配置热加载、多监听器
│   ├── service.go       # system service 封装
│   ├── config.sample.yml
│   └── proxy/           # 代理核心
├── deploy/              # Docker/systemd 部署模板和公开配置
│   ├── docker/
│   ├── systemd/
│   └── config/
├── test/e2e/            # 联调和基准测试脚本
├── scripts/             # 构建、发布和 TPROXY 示例脚本
├── build/               # 普通构建中间产物（忽略）
├── release/             # 正式发布 staging（忽略，每次重建）
└── .local/              # 本地运行配置、日志和数据（忽略）
```

## 核心模块

### 入口层

- `src/main.go`：解析 `-config`、`-version` 和 `run/service/install/uninstall/start/stop/restart` 命令。
- `src/config.go`：加载配置、展开环境变量、补默认值、校验监听地址和上游引用。包含 TLS、多监听器、DNS、流量审计、目标策略、TPROXY 配置。
- `src/config_api_impl.go`：实现 `ConfigEditor` 接口，通过原子写入持久化 YAML 配置变更。
- `src/server.go`：监听 TCP、识别协议、应用配置、管理连接计数、监听配置变更。支持多监听器、TLS 终止、TPROXY 模式。
- `src/service.go`：通过 `kardianos/service` 支持系统服务安装与运行。

### 协议层

- `src/proxy/http.go`：处理 HTTP 请求和 HTTPS CONNECT 隧道。
- `src/proxy/socks.go`：处理 SOCKS5 协商和命令分发。
- `src/proxy/socks_bind.go`：实现 SOCKS5 BIND 和 UDP ASSOCIATE。
- `src/proxy/socks_sniff.go`：通过 TLS SNI / HTTP Host 从首包恢复域名。包含 IPv6 SOCKS5 回复处理。
- `src/proxy/socks5h.go`：socks5h 远端解析逻辑。

### 路由层

- `src/proxy/router.go`：编译与匹配路由规则，执行 direct/proxy/auto 决策。集成 DNS 解析器和 IP 优先级策略。
- `src/proxy/destination_policy.go`：目标地址访问策略，SSRF 防护，保护私网/回环/元数据等敏感地址。

### 上游层

- `src/proxy/upstream.go`：管理上游代理、优先级、权重和选择逻辑。支持 weighted_round_robin/least_connections/consistent_hash/source_ip_hash 策略。
- `src/proxy/upstream_health.go`：执行上游健康检查。支持 TCP/HTTP/HTTPS/TLS 四种检查类型。
- `src/proxy/upstream_dial.go`：上游拨号逻辑。
- `src/proxy/upstream_tunnel.go`：上游隧道转发。

### DNS 层

- `src/proxy/dns.go`：内置 DoH/DoT DNS 解析器，支持 fallback 到系统 DNS，内置 TTL 缓存，支持 IPv4/IPv6 优先级策略。

### 透明代理

- `src/proxy/tproxy.go`：透明代理配置和处理。
- `src/proxy/tproxy_linux.go`：Linux 专用实现（SO_ORIGINAL_DST、IP_TRANSPARENT）。
- `src/proxy/tproxy_stub.go`：非 Linux 平台桩函数。

### 管理与审计

- `src/proxy/management.go`：提供健康检查、指标、状态、reload、流量查询和配置 API 接口。
- `src/proxy/config_api.go`：运行时配置 API（增删查改 upstream/规则/用户）。
- `src/proxy/metrics.go`：维护 Prometheus 文本格式指标，包括流量审计指标。
- `src/proxy/traffic_audit.go`：流量统计器，按 user/upstream/target 三维统计，支持配额限制。
- `src/proxy/access.go`：客户端 CIDR 白名单、单 IP 限速和用户并发限制。
- `src/proxy/logging.go`：初始化结构化日志和日志滚动。
- `src/proxy/timeout.go`：隧道超时和流量统计封装。

## 构建与运行

```bash
cd src
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o k5pw .
./k5pw -config config.sample.yml run
```

Docker：

```bash
cd deploy/docker
docker compose -f compose.local.yml up --build
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
- `listen` 是旧版单端口代理监听地址（与 `listeners` 二选一）。
- `listeners` 是多监听端口列表，每个端口独立配置 TLS、用户、访问控制等。
- `management` 是独立管理端口。
- `management_auth_token` 非空时所有管理接口都要求 Bearer Token。
- 管理端口绑定非回环地址时必须配置 `management_auth_token`。
- `users` 为空时允许匿名代理访问。
- `tls` 配置 TLS 服务端终止（`cert_file`/`key_file`/`client_ca_file`）。
- `upstreams[].types` 支持 `http`、`https`、`socks5`、`socks5h`。
- `upstreams[].strategy` 支持 `weighted_round_robin`、`least_connections`、`consistent_hash`、`source_ip_hash`。
- `upstreams[].health_check` 支持 `tcp`/`http`/`https`/`tls` 四种检查类型。
- `routing.rules` 按顺序匹配，首个命中规则生效。
- `routing.final` 支持 `direct`、`proxy`、`auto`。
- `dns.type` 支持 `system`（默认）、`doh`、`dot`。
- `dns.ip_precedence` 支持 `ipv4_first`（默认）、`ipv6_first`、`system`。
- `destination_policy.protect_private` 默认 true，拒绝回环/私网/链路本地等敏感地址。
- `traffic_audit.enabled` 启用流量审计与配额。
- `tproxy.enabled` 启用透明代理（仅 Linux），支持 `redirect`/`tproxy` 两种模式。
- `geosite.dat` 相对路径按配置文件所在目录解析。
- 配置中的 `${ENV_NAME}` 会被严格展开，缺失或空值会导致启动失败。

## 路由规则

支持的规则类型：

- `domain`
- `domain_suffix`
- `domain_keyword`
- `domain_regex`
- `ip_cidr`（支持 IPv4 和 IPv6 CIDR）
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
- `GET /api/v1/traffic`：返回流量统计快照。
- `GET/POST /api/v1/config/upstreams`：列出/添加上游。
- `PUT/DELETE /api/v1/config/upstreams/{name}`：更新/删除上游。
- `GET/POST /api/v1/config/rules`：列出/添加路由规则。
- `DELETE /api/v1/config/rules/{index}`：删除路由规则。
- `GET/POST /api/v1/config/users`：列出/添加用户。
- `DELETE /api/v1/config/users/{username}`：删除用户。
- `/debug/pprof/*`：仅在 `enable_pprof: true` 时启用。

## 开发注意事项

- 协议入口改动通常需要覆盖 HTTP、CONNECT、SOCKS5 三类路径。
- 路由逻辑改动应补充或更新 `router`、`route_cache`、`socks5h` 相关测试。
- 上游选择逻辑改动应关注优先级、权重、dead 标记、健康检查和指定上游白名单。
- 热加载逻辑改动应确认旧组件停止、新组件原子替换、监听地址切换和管理端口切换。
- 日志初始化可在热加载时发生，避免关闭仍被并发写入的 writer。
- BIND 与 UDP ASSOCIATE 只适合 SOCKS5 上游，HTTP/HTTPS 上游不支持。
- pprof 和管理接口属于敏感入口，不应默认暴露到公网。
- TLS 终止在 listener 层统一包装，协议层无感知。
- TPROXY 仅支持 Linux，非 Linux 平台有桩函数返回错误。
- IPv6 支持通过 Go 标准库原生实现，`ip_precedence` 控制优先级策略。
- 目标策略（`destination_policy`）独立于路由，是 SSRF 防护的最后一道防线。
- 流量审计通过 `TrafficAccounter` 实现，`Tunnel` 函数签名包含 `TrafficKey` 和 `*TrafficAccounter` 参数。
- 运行时配置 API 读取原始 YAML（不展开环境变量），保留 `${ENV_VAR}` 占位符。
