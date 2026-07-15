# YiGate

YiGate 是一个轻量级 Go 语言实现的统一代理网关。HTTP、HTTPS CONNECT 与 SOCKS5 共用单端口接入，通过规则路由、多上游级联、健康检查、socks5h 远端解析、配置热加载和管理接口提供可运维的代理能力。项目定位为合规矩形、轻量可运维的统一代理网关，明确不支持任何混淆/非合规协议。

## 主要特性

### 代理协议

- HTTP、HTTPS CONNECT、SOCKS5 统一端口接入，自动识别下游协议。
- SOCKS5 支持 CONNECT、BIND、UDP ASSOCIATE。
- 支持 HTTP、HTTPS、SOCKS5、SOCKS5H 上游代理级联。
- 支持 socks5h 远端域名解析与 TLS SNI / HTTP Host 目标恢复。

### TLS 服务端终止

- 代理端口支持 HTTPS / SOCKS5 over TLS。
- 支持 mTLS 客户端认证（`client_ca_file`）。
- 可在全局或 per-listener 级别配置。

### 负载均衡

- 多上游支持优先级与加权轮询。
- 支持最少连接（`least_connections`）、一致性哈希（`consistent_hash`）、源 IP 哈希（`source_ip_hash`）策略。
- 支持熔断与失败转移。

### 健康检查

- 支持 TCP、HTTP、HTTPS、TLS 四种检查类型。
- 检查间隔、超时、失败阈值支持 per-upstream 配置。
- 失败上游临时摘除与自动恢复。

### 多监听端口

- 每个端口可独立配置 TLS、用户认证、访问控制、SOCKS5 命令和最大连接数。
- 向后兼容单端口 `listen` 配置。

### DNS 安全解析

- 内置 DoH（DNS-over-HTTPS）和 DoT（DNS-over-TLS）解析器。
- 可配置 fallback 到系统 DNS，内置 60 秒 TTL 缓存。
- 支持 IPv4/IPv6 优先级策略（`ipv4_first`/`ipv6_first`/`system`）。

### 运行时配置 API

- 通过 REST API 动态增删查改 upstream、路由规则、用户。
- 变更持久化到 YAML 配置文件并自动热加载。
- 上游 URL 脱敏输出，所有端点要求管理 Token 认证。

### 流量审计与配额

- 按用户、上游、目标域名三维统计流量。
- 支持每用户和每上游流量配额限制。
- 通过 `/api/v1/traffic` 端点查询，通过 Prometheus 指标暴露。

### 透明代理 / TPROXY

- Linux 下支持 TPROXY 透明代理模式。
- 支持 `redirect`（SO_ORIGINAL_DST）和 `tproxy`（IP_TRANSPARENT）两种模式。
- 提供 iptables/nftables 配置示例脚本。

### IPv6 支持

- 监听、DNS 解析、路由规则、SOCKS5、目标策略全面支持 IPv6。

### 目标地址访问策略

- SSRF 防护：拒绝回环、RFC1918 私网、ULA、链路本地、元数据等敏感地址。
- 支持 deny/allow CIDR、域名、端口列表。
- 直连路径在 DNS 解析后再次校验真实 IP。

### 路由与规则

- 支持 direct、proxy、auto 三种路由动作。
- 支持域名、后缀、关键词、正则、IP CIDR、GEOSITE 与 match 规则。
- 支持 `geosite.dat` 自动更新。
- 路由规则按顺序匹配，首个命中规则生效。

### 运维与管理

- 配置文件保存后自动热加载。
- IP 白名单、单 IP 速率限制、单用户并发限制。
- `/health`、`/metrics`、`/api/v1/status`、`/api/v1/reload`、`/api/v1/traffic` 管理接口。
- 运行时配置 API（`/api/v1/config/*`）。
- 可选 `/debug/pprof` 调试端点。
- JSON / console 日志、文件输出和日志滚动。
- Linux systemd 服务和 Docker 运行。

## 目录结构

```text
.
├── src/                 # Go 源码、模块文件和 Go 测试
│   ├── go.mod
│   ├── main.go          # CLI 入口、版本信息、服务命令
│   ├── config.go        # YAML 配置加载、环境变量展开、校验
│   ├── config_api_impl.go # 运行时配置 API 实现（持久化到 YAML）
│   ├── server.go        # 统一 TCP Server、协议识别、配置热加载、多监听器
│   ├── service.go       # 系统服务封装
│   ├── config.sample.yml
│   └── proxy/           # 代理核心（协议/路由/上游/DNS/审计/管理）
├── deploy/              # Docker/systemd 部署模板和公开配置
│   ├── docker/
│   ├── systemd/
│   └── config/
├── test/e2e/            # 联调和基准测试脚本
├── scripts/             # 构建、发布和 TPROXY 示例脚本
└── docs/
```

## 环境要求

- Go 1.21+
- Linux/macOS/Windows 均可构建；systemd 安装脚本面向 Linux。
- TPROXY 透明代理仅支持 Linux。
- Docker 运行需要 Docker Compose。

## 快速开始

```bash
cd src
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o yigate .
./yigate -config ../deploy/config/config.local.yml run
```

默认命令为 `run`，也可以省略：

```bash
./yigate -config ../deploy/config/config.local.yml
```

查看版本：

```bash
./yigate -version
```

## 常用命令

```bash
# 前台运行
./yigate -config config.yml run

# 安装为系统服务
sudo ./yigate -config /etc/yigate/config.yml install

# 启停系统服务
sudo ./yigate -config /etc/yigate/config.yml start
sudo ./yigate -config /etc/yigate/config.yml stop
sudo ./yigate -config /etc/yigate/config.yml restart

# 卸载系统服务
sudo ./yigate -config /etc/yigate/config.yml uninstall
```

## 配置说明

参考 `src/config.sample.yml`。核心字段如下：

### 监听与认证

- `listen`：代理监听地址（旧版单端口，与 `listeners` 二选一）。
- `listeners`：多监听端口列表，每个端口独立配置 TLS、用户、访问控制等。
- `users`：下游代理认证用户表；为空表示允许匿名访问。
- `tls`：TLS 服务端终止配置（`cert_file`/`key_file`/`client_ca_file`）。

### 管理接口

- `management`：管理接口监听地址；为空则不启用。
- `management_auth_token`：管理接口 Bearer Token；管理端口绑定非回环地址时必须配置。
- `enable_pprof`：是否启用 pprof 管理端点。

### 上游与路由

- `upstreams`：上游代理列表，支持 `http`、`https`、`socks5`、`socks5h`。
- `upstreams[].strategy`：负载均衡策略（`weighted_round_robin`/`least_connections`/`consistent_hash`/`source_ip_hash`）。
- `upstreams[].health_check`：per-upstream 健康检查配置。
- `routing.final`：未命中规则时的默认动作，支持 `direct`、`proxy`、`auto`。
- `routing.rules`：按顺序匹配的路由规则。
- `routing.geosite`：`geosite.dat` 路径与自动更新配置。
- `routing.auto`：自动模式策略、直连探测超时和缓存时间。
- `routing.socks5h_redirect`：优先使用 SOCKS5H 上游进行远端解析。

### DNS 解析

- `dns.type`：DNS 解析模式（`system`/`doh`/`dot`）。
- `dns.doh_url`：DoH 服务器 URL（`type=doh` 时必填）。
- `dns.dot_addr`：DoT 服务器地址（`type=dot` 时必填）。
- `dns.fallback`：DoH/DoT 失败后回退到系统 DNS（默认 true）。
- `dns.ip_precedence`：IPv4/IPv6 优先级（`ipv4_first`/`ipv6_first`/`system`）。

### 目标策略

- `destination_policy.protect_private`：拒绝回环/私网/链路本地等敏感地址（默认 true）。
- `destination_policy.default_action`：非受保护地址默认动作（`allow`/`deny`）。
- `destination_policy.deny_cidrs`/`allow_cidrs`：CIDR 黑白名单。
- `destination_policy.deny_domains`/`allow_domains`：域名黑白名单。
- `destination_policy.deny_ports`/`allow_ports`：端口黑白名单。

### 流量审计

- `traffic_audit.enabled`：启用流量审计。
- `traffic_audit.per_user_quota`：每用户流量配额（字节），0 表示不限制。
- `traffic_audit.per_upstream_quota`：每上游流量配额（字节），0 表示不限制。
- `traffic_audit.quota_window`：配额统计窗口，默认 1 小时。

### 透明代理

- `tproxy.enabled`：启用透明代理（仅 Linux）。
- `tproxy.mode`：透明代理模式（`redirect`/`tproxy`）。
- 也可在 `listeners[].tproxy` 中 per-listener 配置。

### 其他

- `access_control`：客户端白名单、速率限制和用户并发限制。
- `timeouts`：拨号、握手、HTTP 交换和隧道空闲超时。
- `log`：日志级别、格式、输出位置和滚动策略。
- `check_interval`：上游健康检查间隔，单位秒。
- `max_connections`：全局最大下游连接数；为 0 表示不限制。

配置中的密码、Token、上游地址可使用环境变量，例如 `${YIGATE_PROXY_PASSWORD}`。引用的环境变量未设置或为空时，配置加载会失败。

## 管理接口

启用 `management` 后可访问：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/metrics` | GET | Prometheus 指标 |
| `/api/v1/status` | GET | 运行状态摘要 |
| `/api/v1/reload` | POST | 手动重新加载配置 |
| `/api/v1/traffic` | GET | 流量统计快照 |
| `/api/v1/config/upstreams` | GET / POST | 列出 / 添加上游 |
| `/api/v1/config/upstreams/{name}` | PUT / DELETE | 更新 / 删除上游 |
| `/api/v1/config/rules` | GET / POST | 列出 / 添加路由规则 |
| `/api/v1/config/rules/{index}` | DELETE | 删除路由规则 |
| `/api/v1/config/users` | GET / POST | 列出 / 添加用户 |
| `/api/v1/config/users/{username}` | DELETE | 删除用户 |
| `/debug/pprof/*` | GET | pprof 调试端点（需 `enable_pprof: true`） |

配置了 `management_auth_token` 时，请携带请求头：

```text
Authorization: Bearer <token>
```

## Docker 部署

本机匿名模式（仅回环监听）：

```bash
cd deploy/docker
docker compose -f compose.local.yml up --build
```

局域网认证模式（需设置环境变量）：

```bash
export YIGATE_PROXY_PASSWORD=yourpassword
export YIGATE_MANAGEMENT_TOKEN=yourtoken
cd deploy/docker
docker compose -f compose.lan.yml up --build
```

> **安全提示：** 本机模板仅监听 127.0.0.1，允许匿名代理。对外暴露前请使用局域网模板并配置认证。自行配置非回环匿名代理的风险由部署者承担。

## 透明代理部署

透明代理需要 Linux 内核支持，配合 iptables 或 nftables 使用：

```bash
# REDIRECT 模式（简单，不保留源 IP）
bash scripts/tproxy-redirect-iptables.sh

# TPROXY 模式（保留源 IP）
bash scripts/tproxy-tproxy-iptables.sh

# nftables 模式
bash scripts/tproxy-nftables.sh
```

## 测试

```bash
cd src
GOPROXY=https://goproxy.cn,direct go test ./...
```

代理联调脚本位于 `test/e2e/`，运行前需要先启动对应端口的代理服务。

## 交叉编译与发布

项目纯 Go 实现，无 CGO 依赖，可交叉编译到常见平台。

### 支持的目标平台

| OS | 架构 |
|---|---|
| Linux | amd64、arm64、386、armv7、riscv64、ppc64le、s390x、mips64、mips64le |
| macOS | amd64、arm64 |
| Windows | amd64、386、arm64 |
| FreeBSD | amd64、386、arm64 |
| OpenBSD | amd64、arm64 |
| NetBSD | amd64 |

> 注：`install/start/stop/restart/uninstall` 系统服务命令依赖 `kardianos/service`，在 Linux、macOS、Windows 与 FreeBSD 上完整支持；其他 BSD 平台可运行 `run` 模式作为前台代理。

### 本地交叉编译

```bash
# 使用 Makefile 一次性交叉编译所有平台（开发验证）
make cross

# 结果位于 build/ 目录
ls build/
```

### 正式发布

正式发布从已有 Git tag 构建，拒绝脏工作区：

```bash
# 前置：确保 Git 工作区干净，已创建并推送 tag
git tag v1.2.0
git push origin v1.2.0
# CI 自动构建并发布
```

本地验证：

```bash
./scripts/release.sh 1.2.0
./scripts/check-release-files.sh 1.2.0
```

### Windows 本地构建

```powershell
.\scripts\build.ps1 -Version 1.2.0
Get-ChildItem build\
```

### 自动发布（GitHub Actions）

仓库已配置 CI/CD：

- `.github/workflows/ci.yml`：每次 Push / PR 自动运行测试、交叉编译全部目标平台，并验证 Docker 镜像和部署模板。
- `.github/workflows/release.yml`：推送形如 `v1.2.0` 的 tag 时自动运行完整质量门禁，调用 `scripts/release.sh` 生成正式制品并发布到 GitHub Release。

## 安全建议

- 对公网开放代理端口前务必配置强密码和客户端白名单。
- 管理端口如绑定非回环地址必须配置 `management_auth_token`。
- 不要把真实密码、Token、上游账号写入仓库；优先使用环境变量。
- `enable_pprof` 仅建议在排障时临时开启。
- `destination_policy.protect_private` 建议保持开启，防止 SSRF 攻击。
- 透明代理模式需要 root 权限配置 iptables/nftables，生产环境注意最小权限原则。

## 许可证

见 [LICENSE](LICENSE)。
