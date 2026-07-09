# AGENTS.md

## 项目概述

`k5pw` 是一个使用 Go 标准库 + 少量外部依赖实现的轻量级透明代理。支持 HTTP、HTTPS（CONNECT 隧道）和 SOCKS5 三种协议共用一个端口，支持级联、多上游、优先级、加权负载均衡、规则分流、GEOSITE、auto 缓存以及 socks5h 重定向。

## 技术栈

- Go 1.21+
- 依赖：
  - `github.com/fsnotify/fsnotify`（配置热加载）
  - `github.com/kardianos/service`（系统服务管理）
  - `gopkg.in/yaml.v3`（YAML 配置解析）

## 目录结构

```
k5pw/
├── .gitignore
├── AGENTS.md                # 本文件：面向维护者/Agent 的项目说明
├── README.md                # 用户文档
├── docs/                    # 文档目录（可扩展）
├── dist/                    # 生产环境（已加入 .gitignore）
│   ├── docker-compose.yml   # Docker 生产部署编排
│   └── k5/                  # 挂载给容器的配置、geosite.dat、日志
│       ├── config.yml       # 生产配置（端口 8891）
│       ├── geosite.dat
│       └── logs/            # 运行时日志
├── pyscripts/               # 测试脚本（已加入 .gitignore）
│   ├── test_proxy_4479.py   # 开发环境测试脚本（端口 4479）
│   └── test_proxy_8891.py   # 生产环境测试脚本（端口 8891）
└── src/                     # 源码目录
    ├── go.mod
    ├── go.sum
    ├── Dockerfile           # 多阶段构建 Dockerfile
    ├── main.go              # CLI 入口、服务管理命令
    ├── config.go            # YAML 配置加载、默认值与校验
    ├── config_test.go       # 配置相关测试
    ├── config.yml           # 开发环境配置（端口 4479）
    ├── server.go            # 统一 TCP 监听、协议识别、配置热加载
    ├── service.go           # 系统服务注册/启停
    └── proxy/               # 代理协议与路由核心
        ├── auth.go
        ├── upstream.go
        ├── upstream_test.go
        ├── router.go
        ├── router_test.go
        ├── route_cache.go
        ├── route_cache_test.go
        ├── http.go
        ├── socks.go
        ├── socks_sniff.go
        ├── socks_sniff_test.go
        ├── socks_addr.go
        ├── socks5h.go
        ├── geosite.go
        ├── geosite_test.go
        ├── geosite_update.go
        ├── geosite_update_test.go
        └── timeout.go
```

## 构建与运行

本地二进制（开发环境）：

```bash
cd src
go build -o k5pw
./k5pw -config config.yml
```

默认监听 `0.0.0.0:4479`（开发配置），可用 `-config` 指定其他配置文件。

生产环境（Docker，当前环境可能因镜像拉取受限而无法使用）：

```bash
cd dist
docker compose up --build
```

Docker 模式下构建上下文为 `../src`，配置与数据挂载到 `dist/k5`，日志写入 `dist/k5/logs/k5pw.log`。

## 测试

```bash
# 全量测试
cd src && go test ./...

# 运行开发环境联调测试（需先启动 4479 端口代理）
python3 pyscripts/test_proxy_4479.py

# 运行生产环境联调测试（需先启动 8891 端口代理）
python3 pyscripts/test_proxy_8891.py
```

## 核心架构

`k5pw` 的核心是一个单进程 TCP 代理服务器，通过首字节识别协议类型，将连接分派给 HTTP 或 SOCKS5 处理器，再由路由器和上游管理器决定如何建立到目标站点的连接。

### 连接入口与协议识别

`Server`（`server.go`）监听单个 TCP 端口，每个连接到来时先设置握手超时，然后使用 `bufio.Reader.Peek(1)` 预读第一个字节。首字节为 `0x05` 时识别为 SOCKS5，否则按 HTTP 处理。这种设计让 HTTP/HTTPS/SOCKS5 三种下游协议可以共用同一个端口。`Server` 还维护活跃连接计数，通过原子操作实现最大并发限制，并支持优雅停止。

### 配置热加载

`Server` 使用 `fsnotify` 监视配置文件所在目录。当配置文件被写入、创建或重命名时，会触发 300ms 防抖，然后调用 `LoadAndApply` 重新加载并应用新配置。配置热加载会替换上游管理器、路由器和 `geosite.dat` 更新器，但监听地址变更不会自动生效，需要重启服务。

### 配置与默认值

`Config`（`config.go`）定义了整个代理的运行时配置。`LoadConfig` 先填充默认值，再解析 YAML 覆盖，并相对配置文件目录解析 `geosite.dat` 路径。`validateConfig` 校验监听地址、上游 URL、权重范围等。

### 上游管理

`UpstreamManager`（`proxy/upstream.go`）负责管理所有上游代理。它将上游按 `priority` 分组，同优先级内使用平滑加权轮询选择。每个上游都有 `dead` 标记，当某个上游 Dial 失败时会被临时标记为 dead；健康检查会定期 TCP 拨号，发现上游恢复后重新加入候选池。`UpstreamManager.Dial(target)` 会尝试所有可用上游，全部失败后再回退到自身直连。路由规则可以通过 `upstreams` 字段限定可选上游白名单。

### 路由决策

`Router`（`proxy/router.go`）按顺序匹配规则，命中第一条即返回决策。支持的规则类型包括 `domain`、`domain_suffix`、`domain_keyword`、`domain_regex`、`ip_cidr`、`geosite` 和 `match`。动作包括 `direct`（直连）、`proxy`（走上游）和 `auto`（自动选择）。`auto` 模式会根据 `direct_first` 或 `proxy_first` 策略先尝试一种方式，成功后把结果缓存到 `RouteCache` 中，后续连接优先复用缓存决策。`geosite` 规则会惰性加载 `geosite.dat` 中实际被引用的 code 集合，减少内存占用。

### 连接建立

`Router.Dial` 根据决策结果选择建立连接的方式：

- `direct`：直接 `net.DialTimeout` 到目标。
- `proxy`：通过 `UpstreamManager.DialProxy` 选择限定上游，失败时尝试下一个。
- `auto`：根据 `auto.mode` 选择先直连还是先代理，成功则缓存。

### HTTP/HTTPS 代理处理

`HandleHTTP`（`proxy/http.go`）处理下游 HTTP 请求。`CONNECT` 方法用于建立 HTTPS 隧道，会返回 `200 Connection Established` 后进入 `Tunnel` 双向转发。普通 HTTP 请求会缓冲最多 1 MiB 请求体以支持上游失败重试，超过则返回 413。普通请求会根据路由决策选择直连、代理或 auto，并支持 `socks5h_redirect` 模式下的优先 socks5h 转发。

### SOCKS5 代理处理

`HandleSOCKS5`（`proxy/socks.go`）处理 SOCKS5 协商。支持匿名和用户名密码认证，实现 `CONNECT`、`BIND` 和 `UDP ASSOCIATE` 三种命令。当目标为 IP 且开启 `socks5h_redirect` 时，会先回复连接成功，然后由 `socks_sniff.go` 嗅探客户端首包的 TLS SNI 或 HTTP Host，恢复域名后重新按域名路由。

### SOCKS5 BIND 与 UDP ASSOCIATE

- `BIND`（`proxy/socks_bind.go`）：服务器监听一个随机端口，等待目标连接，建立后进入双向转发。
- `UDP ASSOCIATE`（`proxy/socks_bind.go`）：服务器监听一个 UDP 端口，接收客户端的 SOCKS5 UDP 数据报，去掉头部后转发到目标，并将目标回复加上 SOCKS5 UDP 头部返回客户端。不支持分片（FRAG > 0）。

### socks5h 重定向

`socks5h_redirect` 开启后，k5pw 在内部优先使用 SOCKS5/socks5h 上游的域名地址类型转发目标连接，让域名在上游侧解析。如果没有可用的 SOCKS5 上游或连接失败，会自动回退到原有规则分流逻辑。该模式会缓存每个目标站点成功使用的 SOCKS5 上游。

### 数据转发

`Tunnel`（`proxy/upstream.go`）在客户端与目标连接之间双向转发数据，使用 `deadlineReader`/`deadlineWriter` 根据 `tunnel_idle` 超时控制空闲连接，支持 `CloseWrite`/`CloseRead` 优雅半关闭。

## 常见任务

- 修改开发端口：改 `src/config.yml` 中的 `listen`。
- 修改生产端口：改 `dist/k5/config.yml` 中的 `listen`，并同步调整 `dist/docker-compose.yml` 的 `ports` 映射。
- 添加上游：在对应 `config.yml` 的 `upstreams` 列表中增加条目。
- 切换上游协议：修改 `url` 的 scheme，或在 `types` 中显式指定。
- 调试：查看日志中的 `upstream ... dead` / `upstream ... recovered`。

## 测试注意事项

- 上游不可达时，代理会自动回退到自身直连。
- 通过 HTTP/HTTPS 上游建立 CONNECT 隧道时，仅读取响应头，不再消费隧道数据，避免隧道建立后阻塞。
- 普通 HTTP 请求默认缓冲最多 1 MiB 请求体以支持上游失败重试；chunked 编码或超过 1 MiB 的请求体会改为直接透传，透传时无法重试。
- 健康检查会向上游发送最小协议握手（HTTP/SOCKS5），只有协议层响应正常才视为可用。
- 监听地址变更后保存配置即可热加载，无需重启服务。
- 当前 Docker 镜像拉取可能受网络环境限制，建议优先使用本地二进制测试。

## 已知限制

- `SOCKS5 BIND` 和 `UDP ASSOCIATE` 仅支持本地直连场景，暂未通过上游代理级联。
- 监听地址变更后保存配置即可热加载，无需重启服务（端口切换期间会短暂中断连接）。
