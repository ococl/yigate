# AGENTS.md

## 项目概述

YiGate 是一个 Go 语言实现的轻量级统一代理网关。核心能力是让 HTTP、HTTPS CONNECT 和 SOCKS5 共用单端口接入，并通过规则路由、多上游、健康检查、socks5h 远端解析、配置热加载和管理接口提供可运维的代理能力。项目定位为合规、轻量、可运维的统一代理网关，明确不支持任何混淆/非合规协议。

## 工作约定

- 所有修改在当前工作区原地完成，不使用 WorkTree。
- 优先保持改动小而聚焦，避免无关重构。
- 修改代码后优先运行相关 Go 测试。
- 下载 Go 依赖时优先使用国内镜像：`GOPROXY=https://goproxy.cn,direct`。
- 不要提交真实代理密码、管理 Token、上游账号或私有地址。

## 技术栈

- Go 1.21+（CI 与构建镜像使用 Go 1.27.3）
- `fsnotify`：配置文件热加载。
- `kardianos/service`：系统服务管理。
- `zap`：结构化日志。
- `lumberjack`：按大小滚动日志。
- `yaml.v3`：YAML 配置解析。
- 前端：Vue 3 + Vite + TypeScript + antdv-next + Pinia + Vue Router（位于 `web/`，独立管理）。

## 重要目录

```text
.
├── src/                 # Go 模块根目录（应用后端，module yigate）
│   ├── go.mod
│   ├── main.go          # CLI 入口、版本信息、服务命令
│   ├── config.go        # YAML 配置加载、环境变量展开、校验
│   ├── config_api_impl.go # 运行时配置 API 实现（持久化到 YAML）
│   ├── server.go        # 统一 TCP Server、协议识别、配置热加载、多监听器
│   ├── service.go       # system service 封装
│   ├── config.sample.yml
│   ├── proxy/           # 代理核心
│   └── proxy/webui_dist/ # 前端构建产物（go:embed 嵌入二进制）
├── web/                 # 前端项目（独立自治，非 monorepo）
├── deploy/             # Docker/systemd 部署模板和公开配置
│   ├── docker/          # Dockerfile、entrypoint.sh、compose.local.yml、compose.lan.yml
│   ├── systemd/         # yigate.service、install.sh
│   └── config/          # 配置模板（config.local.yml / config.lan.yml）
├── test/e2e/            # 联调和基准测试脚本
├── scripts/             # 构建、发布、平台清单、TPROXY 示例脚本
├── docker-compose.yml   # 一键本地部署
├── build/               # 普通构建中间产物（忽略）
└── release/             # 正式发布 staging（忽略，每次重建）
```

## 运行时目录（二进制与 Docker 一致）

二进制原生运行与 Docker 容器使用相同的目录布局，便于在两种形态间切换：

```text
.
├── data/
│   ├── config.yml      # 运行配置（默认路径；首次启动自动从示例生成）
│   └── geosite.dat     # GeoSite 数据（routing.geosite.path 默认 ../data/geosite.dat）
└── logs/
    └── yigate.log      # 滚动日志（log.file 默认 logs/yigate.log）
```

- 默认 `-config` 值为 `data/config.yml`（相对于启动工作目录）。
- Docker 中映射为 `./data:/var/lib/yigate/data:rw` 与 `./logs:/var/lib/yigate/logs`。
- 配置文件相对路径（如 `geosite.dat`）相对于配置文件所在目录解析；`log.file` 相对于启动工作目录解析。

## 核心模块

### 入口层

- `src/main.go`：解析 `-config`、`-version` 和 `run/service/install/uninstall/start/stop/restart` 命令。
- `src/config.go`：加载配置、展开环境变量、补默认值、校验监听地址和上游引用。包含 TLS、多监听器、DNS、流量审计、目标策略、TPROXY 配置。
- `src/config_api_impl.go`：实现 `ConfigEditor` 接口，通过原子写入持久化 YAML 配置变更。
- `src/server.go`：监听 TCP、识别协议、应用配置、管理连接计数、监听配置变更。支持多监听器、TLS 终止、TPROXY 模式；热加载先关闭旧 listener 再创建新 listener。
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
- `src/proxy/metrics.go`：维护 Prometheus 文本格式指标，包括流量审计指标。指标前缀为 `yigate_`。
- `src/proxy/traffic_audit.go`：流量统计器，按 user/upstream/target 三维统计，支持配额限制。
- `src/proxy/access.go`：客户端 CIDR 白名单、单 IP 限速和用户并发限制。
- `src/proxy/logging.go`：初始化结构化日志和日志滚动。
- `src/proxy/timeout.go`：隧道超时和流量统计封装。

## 构建与运行

Go 应用：

```bash
cd src
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o yigate .
./yigate -config data/config.yml run
```

前端（`web/` 目录，独立管理；Node 版本见 `.nvmrc`，依赖安装统一用 `npm ci` 以保证与 `package-lock.json` 一致）：

```bash
cd web
npm ci            # 按锁文件安装依赖
npm run dev       # 开发服务器
npm run build     # 生产构建（输出到 web/dist/）
node scripts/embed-dist.mjs dist ../src/proxy/webui_dist  # 压缩资源并准备嵌入
npm run preview   # 预览构建产物
```

`src/proxy/webui_dist/index.html` 是被跟踪的占位页（保证纯 `go build`/`go vet` 的 `//go:embed` 有文件可嵌入），
`make build` 会用真实产物覆盖它；构建后可用 `git checkout -- src/proxy/webui_dist/index.html` 还原，保持工作区干净。

或通过 Makefile：

```bash
make build        # 构建 Go 应用
make web-install  # 安装前端依赖
make web-build    # 构建前端
make web-dev      # 启动前端开发服务器
```

Docker（一键本地部署）：

```bash
docker compose up --build
```

发布构建：

```bash
./scripts/release.sh 1.2.0
```

## 测试

Go 应用：

```bash
cd src
GOPROXY=https://goproxy.cn,direct go test ./...
```

如果只改 `src/proxy`，仍建议至少运行 `go test ./proxy`。改动配置、启动、热加载或管理接口时，运行 `go test ./...`。

前端：

```bash
cd web
npm run type-check  # TypeScript 类型检查
npm run build       # 构建即验证
```

## 配置要点

- 示例配置：`src/config.sample.yml`。
- `listen` 是旧版单端口代理监听地址（与 `listeners` 二选一）。
- `listeners` 是多监听端口列表，每个端口独立配置 TLS、用户、访问控制等；顶层 `users`/`access_control` 仅被未显式配置的 listener 继承（`users: {}` 表示显式匿名），顶层 `max_connections` 作为兜底值。
- `management` 是独立管理端口，须为合法 `host:port`。
- `management_auth_token` 非空时所有管理接口都要求 Bearer Token；支持热加载（同一端口原地重建路由，无需重启）。
- 管理端口绑定非回环地址时必须配置 `management_auth_token`。
- `webui.enabled` 控制内置前端（默认 true），支持热加载。
- `enable_pprof` 控制 `/debug/pprof/*`（默认 false），支持热加载。
- `users` 为空时允许匿名代理访问。
- `tls` 配置 TLS 服务端终止（`cert_file`/`key_file`/`client_ca_file`）。
- `upstreams[].types` 支持 `http`、`https`、`socks5`、`socks5h`。
- `upstreams[].strategy` 支持 `weighted_round_robin`、`least_connections`、`consistent_hash`、`source_ip_hash`。
- `upstreams[].health_check` 支持 `tcp`/`http`/`https`/`tls`/`socks5`/`socks5h`；留空时 socks5 上游默认按协议握手探测，其余按 TCP 探测。
- `routing.rules` 按顺序匹配，首个命中规则生效。
- `routing.final` 支持 `direct`、`proxy`、`auto`、`deny`。
- `routing.auto.mode` 支持 `direct_first`（默认）、`proxy_first`，非法值加载时报错。
- `routing.socks5h_redirect` 控制目标域名是否交由上游解析。
- `dns.type` 支持 `system`（默认）、`doh`、`dot`；`dns.fallback` 默认 true。
- `dns.ip_precedence` 支持 `ipv4_first`（默认）、`ipv6_first`、`system`。
- `destination_policy` 支持 `protect_private`（默认 true）、`default_action`、`allow/deny_cidrs`、`allow/deny_domains`、`allow/deny_ports`。
- `timeouts` 中 0 表示使用内置默认值（热加载时会把旧值重置为默认）。
- `check_interval` 为 0 表示停止周期性健康检查。
- `log` 支持级别、格式、输出与轮转参数（`log.file` 相对启动工作目录）。
- `traffic_audit.enabled` 启用流量审计与配额。
- `tproxy.enabled` 启用透明代理（仅 Linux），支持 `redirect`/`tproxy` 两种模式（大小写不敏感）。
- `geosite.dat` 相对路径按配置文件所在目录解析。
- 配置中的 `${ENV_NAME}` 会被严格展开，缺失或空值会导致启动失败。
- 加载阶段统一校验监听地址、TLS 文件、上游 URL/策略/健康检查、访问控制数值、路由枚举与目标策略端口，非法配置直接启动失败。

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
- `GET /api/v1/status`：返回运行摘要（含版本与运行时长）。
- `GET /api/v1/version`：返回版本信息。
- `POST /api/v1/reload`：手动重新加载配置。
- `GET /api/v1/traffic`：返回流量统计快照（`enabled` 表示审计开关）。
- `GET/POST /api/v1/config/upstreams`：列出/添加上游（含健康检查配置与 `enabled` 开关）。
- `PUT/DELETE /api/v1/config/upstreams/{name}`：更新/删除上游；更新为局部更新，`enabled=false` 时暂停调度与健康检查。
- `GET/POST /api/v1/config/rules`：列出/添加路由规则。
- `PUT /api/v1/config/rules/{index}`：更新路由规则。
- `POST /api/v1/config/rules/{index}/move`：上移/下移路由规则。
- `DELETE /api/v1/config/rules/{index}`：删除路由规则。
- `GET/POST /api/v1/config/users`：列出/添加用户。
- `PUT /api/v1/config/users/{username}`：修改用户密码。
- `DELETE /api/v1/config/users/{username}`：删除用户。
- `GET/PUT /api/v1/config/settings`：读取/部分更新全局设置（含 `webui_enabled` 与 `management_auth_token`；多 listener 模式下 socks5/访问控制/并发上限的修改会同步到各 listener）。
- `GET /api/v1/status` 返回 `pprof_enabled` 与 `webui_enabled`，前端据此展示开关状态。
- 配置写入前校验，热加载失败时自动回滚配置文件。
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
- Prometheus 指标前缀为 `yigate_`，环境变量前缀为 `YIGATE_`。
- 前端代码位于 `web/`，独立于 Go 应用，有自己的 `package.json` 和构建流程，非 monorepo。
- 前端使用 antdv-next 组件库，通过 `unplugin-vue-components` + `@antdv-next/auto-import-resolver` 实现按需自动导入。
- 前端开发服务器默认代理 `/api` 请求到 `http://127.0.0.1:8890`（Go 管理端口）。
- 跨平台构建目标清单见 `scripts/platforms.sh`（当前 26 个平台），CI 使用 CNB（`.cnb.yml`）。
- 监听使用 `SO_REUSEADDR`/`SO_REUSEPORT`（Linux），由 `reuseAddrListen` 实现（build tag 拆分到 `server_unix.go`/`server_windows.go`）。
