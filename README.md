# k5pw 简易透明代理

一个使用 Go 标准库 + `gopkg.in/yaml.v3` 实现的简易代理服务器，支持 HTTP、HTTPS（CONNECT 隧道）和 SOCKS5 三种协议共用同一个端口，支持级联、多上游、优先级、加权负载均衡、规则分流、GEOSITE 以及 auto 缓存。

## 功能

- **统一端口**：HTTP/HTTPS/SOCKS5 共用一个监听端口，自动识别协议。
- **协议支持**：普通 HTTP 请求转发、HTTPS CONNECT 隧道、SOCKS5 CONNECT/BIND/UDP ASSOCIATE。
- **级联与多上游**：可配置多个上级代理，支持 HTTP、HTTPS、SOCKS5 三种上游。
- **规则分流**：支持 `direct`、`proxy`、`auto` 三种动作，可按域名、域名后缀、关键词、正则、IP CIDR、GEOSITE 匹配。
- **GEOSITE**：可直接读取并自动更新 v2ray/xray/sing-box 生态的 `geosite.dat`，例如使用 `GEOSITE:CN` 让国内站点直连。
- **Socks5h 重定向**：可开启全量下游请求优先走 SOCKS5 上游远端解析，失败时自动回退原有分流。
- **Auto 缓存**：auto 模式会缓存站点的直连或具体上游结果，下次访问优先复用。
- **优先级与权重**：同一批候选上游内，优先使用 `priority` 更小的组，组内按 `weight` 平滑加权轮询。
- **访问控制**：支持 `allowed_clients` CIDR 白名单、单 IP 速率限制、单用户并发连接数限制。
- **管理端口**：可选独立管理端口，提供 `/health`（健康检查）与 `/metrics`（Prometheus 指标）。
- **pprof 调试**：管理端口可选启用 `/debug/pprof` 用于线上 CPU/内存/协程分析。
- **结构化日志**：使用 `zap` 输出 JSON/Console 结构化日志，支持文件滚动、归档压缩和日志级别调整。
- **版本信息**：`-version` 可输出版本号、commit、构建时间。
- **配置热加载**：通过 `fsnotify` 监视配置文件，保存后自动重载。

## 目录结构

```
k5pw/
├── src/              # Go 源码与开发配置
│   ├── config.yml    # 开发环境配置（默认端口 4479）
│   ├── main.go
│   ├── config.go
│   ├── server.go
│   ├── service.go
│   ├── proxy/        # 代理协议与路由核心
│   └── Dockerfile
├── dist/             # 生产环境（已加入 .gitignore）
│   ├── docker-compose.yml
│   └── k5/           # 配置、geosite.dat、日志挂载目录
│       ├── config.yml
│       ├── geosite.dat
│       └── logs/
├── pyscripts/        # 测试脚本（已加入 .gitignore）
│   ├── test_proxy_4479.py
│   └── test_proxy_8891.py
├── docs/                    # 文档目录
│   ├── config_schema.md     # 完整配置字段说明
│   ├── troubleshooting.md   # 故障排查手册
│   └── remediation_plan.md  # 产品化修复计划
├── CHANGELOG.md             # 版本变更日志
├── README.md                # 用户文档
├── AGENTS.md                # 面向维护者/Agent 的项目说明
```

## 快速开始

本地二进制（开发环境）：

```bash
cd src
go build -o k5pw
./k5pw -config config.yml
```

默认监听 `0.0.0.0:4479`，可修改 `src/config.yml` 调整配置。

### 多平台 release 构建

```bash
./scripts/release.sh 1.2.0
```

脚本会在 `build/` 目录生成 Linux、Darwin、Windows、FreeBSD 多架构二进制，并自动创建 Git tag `v1.2.0`。推送到远程后，GitHub Actions（`.github/workflows/release.yml`）会自动上传产物到 Release 页面。

生产环境（Docker，当前环境可能因镜像拉取受限而无法使用）：

```bash
cd dist
docker compose up --build
```

生产配置在 `dist/k5/config.yml`，默认监听 `0.0.0.0:8891`，日志写入 `dist/k5/logs/k5pw.log`。

## 测试

```bash
# 全量 Go 测试
cd src && go test ./...

# 开发环境联调（需先启动 4479 端口代理）
python3 pyscripts/test_proxy_4479.py

# 严格模式：站点关键词缺失也视为失败
python3 pyscripts/test_proxy_4479.py --strict

# 生产环境联调（需先启动 8891 端口代理）
python3 pyscripts/test_proxy_8891.py

# 自定义端口测试
python3 pyscripts/test_proxy_4479.py --port 8891
```

## 配置示例

```yaml
listen: "0.0.0.0:4479"
users:
  username: "your-password"

upstreams:
  - name: "hk"
    url: "socks5://your-upstream-ip:port"
    types: ["socks5", "socks5h"]
    priority: 1
    weight: 5
  - name: "sg"
    url: "socks5://${UPSTREAM_USER}:${UPSTREAM_PASS}@your-upstream-ip:port"
    # 未写 types 时，使用 url 的 scheme 作为默认类型
    priority: 1
    weight: 3

routing:
  final: auto
  geosite:
    path: "geosite.dat"
    auto_update: true
    update_url: "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
    update_interval: 24h
  auto:
    mode: direct_first
    direct_timeout: 1500ms
    cache_ttl: 1h
  socks5h_redirect:
    enabled: true
  rules:
    - type: geosite
      values: ["CN"]
      action: direct
    - type: ip_cidr
      values:
        - "10.0.0.0/8"
        - "172.16.0.0/12"
        - "192.168.0.0/16"
        - "127.0.0.0/8"
        - "100.64.0.0/10"
      action: direct
    - type: domain_suffix
      values:
        - "github.com"
        - "githubusercontent.com"
        - "google.com"
        - "gstatic.com"
        - "docker.io"
        - "docker.com"
      action: proxy

timeouts:
  dial: 10s
  handshake: 10s
  http_exchange: 60s
  tunnel_idle: 5m

log:
  level: info
  format: json
  output: file
  file: "logs/k5pw.log"
  rotation: size
  max_size: 100
  max_age: 7
  max_backups: 10
  compress: true

check_interval: 30
max_connections: 1024
```

## 配置说明

- `listen`：监听地址，例如 `127.0.0.1:4479`；如需对外提供服务，可绑定 `0.0.0.0:4479`。
- `management`：可选管理端口监听地址，例如 `127.0.0.1:8892`。配置后提供：
  - `/health`：进程健康检查，组件未就绪时返回 503。
  - `/metrics`：Prometheus 格式监控指标。
  - `GET /api/v1/status`：进程运行摘要（监听地址、上游数量、组件就绪状态等）。
  - `POST /api/v1/reload`：手动触发热加载。
- `enable_pprof`：是否在管理端口注册 `/debug/pprof` 系列路由，默认 `false`。
- `users`：用户名密码映射。留空或删除表示匿名。
- `upstreams`：上游代理列表。
  - `name`：上游名称，可被路由规则的 `upstreams` 引用；不写会自动生成 `upstream-1`、`upstream-2`。
  - `url`：上游地址。认证信息**强烈建议**通过环境变量注入，例如 `socks5://${UPSTREAM_USER}:${UPSTREAM_PASS}@host:port`；k5pw 会在加载配置时使用 `os.ExpandEnv` 替换 URL 中的环境变量。未配置 `types` 时默认使用 URL scheme 作为初始类型。
  - `types`：可选的代理类型数组，支持 `http`、`https`、`socks5`、`socks5h` 任意组合；用于补充 URL scheme 之外的可用协议。
  - `priority`：数值越小优先级越高。
  - `weight`：同优先级下的权重，越大被选中的概率越高。
- `routing.final`：所有规则都不匹配时的动作，支持 `direct`、`proxy`、`auto`，默认 `auto`。
- `routing.geosite.path`：`geosite.dat` 路径。相对路径会按配置文件所在目录解析。
- `routing.geosite.auto_update`：是否在启动时和定时任务中自动更新 `geosite.dat`，默认通过上游代理 auto 选择下载。
- `routing.geosite.update_url`：`geosite.dat` 下载地址，可改成自建镜像或其他兼容数据源；默认使用 v2fly `domain-list-community` 的 `dlc.dat`。
- `routing.geosite.update_interval`：自动更新周期，默认 `24h`。
- `routing.auto.mode`：`direct_first` 或 `proxy_first`，默认 `direct_first`。
- `routing.auto.direct_timeout`：auto 直连快速探测超时，例如 `1500ms`、`2s`。
- `routing.auto.cache_ttl`：auto 缓存有效期，例如 `1h`。
- `routing.socks5h_redirect.enabled`：开启后所有下游请求优先经 SOCKS5/socks5h 上游以域名方式转发；若没有可用 SOCKS5 上游或连接失败，自动回退原有规则分流。
- `routing.socks5h_redirect.upstreams`：可选的 SOCKS5 上游名称白名单；不配置表示使用全部 SOCKS5/socks5h 上游。
- `routing.rules`：按顺序匹配，命中第一条即返回。
- `timeouts.dial`：拨号超时，默认 `10s`。
- `timeouts.handshake`：协议握手超时，默认 `10s`。
- `timeouts.http_exchange`：普通 HTTP 请求交换超时，默认 `60s`。
- `timeouts.tunnel_idle`：隧道空闲超时，默认 `5m`。
- `log`：日志输出规则。
  - `level`：日志级别，支持 `debug`、`info`、`warn`、`error`、`fatal`、`panic`，默认 `info`。
  - `format`：输出格式，`json` 或 `console`，默认 `json`。
  - `output`：输出目标，`stdout`、`stderr` 或 `file`，默认 `stdout`。
  - `file`：文件路径，`output: file` 时必填。
  - `rotation`：滚动模式，`size`（按大小分包）或 `daily`（按自然日滚动），默认 `size`。
  - `max_size`：`size` 模式下单个文件大小上限（MB），默认 `100`。
  - `max_age`：历史日志保留天数，默认 `7`；过期日志自动删除。
  - `max_backups`：`size` 模式下最多保留的滚动文件数，默认 `10`。
  - `compress`：是否对历史日志进行 gzip 压缩，默认 `true`。
- `access_control`：访问控制。
  - `allowed_clients`：客户端 CIDR 白名单，空表示允许所有。
  - `rate_limit.connections_per_second`：单个 IP 每秒新建连接数，`0` 表示不限速。
  - `rate_limit.burst`：令牌桶突发容量，`0` 时使用 `connections_per_second`。
  - `max_conns_per_user`：单个认证用户最大并发连接数，`0` 表示不限制。
- `check_interval`：健康检查周期，单位秒，默认 30；设置为 0 禁用健康检查。健康检查会向上游发送最小协议握手（HTTP/SOCKS5），只有协议层响应正常才视为可用。
- `max_connections`：最大并发连接数，默认 1024；设置为负数会拒绝启动。

## 环境变量注入上游凭据

为避免将上游代理的用户名、密码明文写入配置文件，k5pw 支持在 `upstreams[].url` 中使用 `${ENV_NAME}` 语法引用环境变量。启动前设置对应环境变量即可：

```bash
export UPSTREAM_USER="your-username"
export UPSTREAM_PASS="your-password"
./k5pw -config config.yml
```

配置文件中写法：

```yaml
upstreams:
  - name: "sg"
    url: "socks5://${UPSTREAM_USER}:${UPSTREAM_PASS}@your-upstream-ip:port"
```

> 注意：环境变量名可自定义，只要与配置文件中 `${}` 内的名称一致即可。

## 路由规则

规则结构：

```yaml
- type: domain_suffix
  values: ["github.com"]
  action: proxy
  upstreams: ["hk", "sg"]
```

支持的 `type`：

- `domain`：完整域名匹配。
- `domain_suffix`：域名后缀匹配，`github.com` 可匹配 `github.com` 和 `api.github.com`。
- `domain_keyword`：域名包含关键词。
- `domain_regex`：域名正则匹配。
- `ip_cidr`：目标为 IP 时匹配 CIDR。
- `geosite`：匹配 `geosite.dat` 中的规则集，例如 `values: ["CN"]`。
- `match`：兜底匹配。

支持的 `action`：

- `direct`：强制直连。
- `proxy`：强制走上游；如果配置了 `upstreams`，只在这些上游里选择；如果不配置 `upstreams`，按全局上游优先级和权重自动选择并重试。
- `auto`：先按 `routing.auto.mode` 尝试直连或上游，成功后缓存该站点结果。

## GEOSITE

`geosite` 规则直接解析 v2ray/xray/sing-box 生态的 `geosite.dat`，不需要维护 txt 列表。常用方式：

```yaml
routing:
  geosite:
    path: "geosite.dat"
  rules:
    - type: geosite
      values: ["CN"]
      action: direct
```

启动时只会加载配置中实际引用的 code，例如 `CN`、`GOOGLE`、`GITHUB`。如果配置了 geosite 规则但文件不存在，程序会拒绝启动，避免误以为规则已生效。

开启 `auto_update` 后，程序会在启动时先尝试更新 `geosite.dat`，随后按 `update_interval` 定时更新。更新请求会优先使用当前上游代理列表的 auto 选择；如果没有配置上游才会直连下载。`update_url` 可以自定义为内网镜像或其他可访问地址。

## Socks5h 重定向

`socks5h_redirect.enabled` 开启后，k5pw 不会改变客户端使用的 HTTP/SOCKS5 协议，而是在内部优先使用 SOCKS5/socks5h 上游的域名地址类型转发目标连接，效果等价于客户端直接使用 `socks5h`：目标域名由上游侧解析，减少本机 DNS 解析。

该模式会缓存每个目标站点成功使用的 SOCKS5 上游；缓存命中时直接复用该上游。如果所有 SOCKS5/socks5h 上游不可用，或本次连接失败，会自动回退到原有规则分流逻辑。

## 使用示例

### HTTP 代理

```bash
curl -x http://username:your-password@127.0.0.1:4479 https://www.example.com
```

### SOCKS5 代理

```bash
curl -x socks5://username:your-password@127.0.0.1:4479 https://www.example.com
```

## 注意

- 本项目仅用于学习、测试或内部网络管理，请勿用于非法用途。
- 上游代理的认证信息建议通过环境变量注入，避免明文写入配置文件或提交到版本仓库。
- 已实现 SOCKS5 CONNECT、BIND 和 UDP ASSOCIATE；BIND 与 UDP ASSOCIATE 支持通过 SOCKS5 上游级联，级联失败时自动回退本地直连。
- 普通 HTTP 请求默认缓冲最多 1 MiB 请求体以支持重试；chunked 编码或超过 1 MiB 的请求体会改为直接透传，透传时无法重试。
- `GEOSITE:CN` 是域名分流，不需要提前 DNS 解析；GEOIP 暂未实现。
- 监听地址变更后保存配置即可热加载，无需重启服务。
- 当前 Docker 镜像拉取可能受网络环境限制，建议优先使用本地二进制测试。

## 相关文档

- [`docs/config_schema.md`](docs/config_schema.md)：完整配置字段说明。
- [`docs/troubleshooting.md`](docs/troubleshooting.md)：故障排查手册。
- [`CHANGELOG.md`](CHANGELOG.md)：版本变更日志。
- [`AGENTS.md`](AGENTS.md)：面向维护者/Agent 的项目说明。
