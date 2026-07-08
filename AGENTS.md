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
/export/app/k5pw/
├── main.go                  # CLI 入口、服务管理命令
├── config.go                # YAML 配置加载、默认值与校验
├── config_test.go           # 配置相关测试
├── server.go                # 统一 TCP 监听、协议识别、配置热加载
├── service.go               # 系统服务注册/启停
├── go.mod
├── go.sum
├── config.yml               # 运行时配置文件
├── README.md                # 用户文档
├── AGENTS.md                # 本文件：面向维护者/Agent 的项目说明
└── proxy/                   # 代理协议与路由核心
    ├── auth.go              # HTTP Basic / SOCKS5 用户名密码认证
    ├── upstream.go          # 上游管理、优先级、加权轮询、健康检查、级联 Dial
    ├── upstream_test.go
    ├── upstream_probe.go        # （已移除）上游协议异步探测与缓存
    ├── upstream_probe_test.go
    ├── router.go            # 路由规则、auto 模式、分流决策
    ├── router_test.go
    ├── route_cache.go       # 直连/代理决策缓存
    ├── route_cache_test.go
    ├── http.go              # HTTP/HTTPS 代理处理
    ├── socks.go             # SOCKS5 代理处理
    ├── socks_sniff.go       # SOCKS5 连接 TLS SNI / HTTP Host 嗅探与域名重写
    ├── socks_sniff_test.go
    ├── socks_addr.go        # SOCKS5 地址编码/解码
    ├── socks5h.go           # socks5h 重定向逻辑
    ├── geosite.go           # geosite.dat 解析与匹配
    ├── geosite_test.go
    ├── geosite_update.go    # geosite.dat 自动下载与更新
    ├── geosite_update_test.go
    └── timeout.go           # 超时与连接空闲控制
```

## 构建与运行

```bash
cd /export/app/k5pw
go build -o k5pw
./k5pw -config config.yml
```

默认监听 `127.0.0.1:8891`，可用 `-config` 指定其他配置文件。

## 配置说明

```yaml
listen: "127.0.0.1:8891"
users: {}
upstreams:
  - url: "socks5://10.2.3.150:7897"
    types: ["socks5", "socks5h"]
    priority: 1
    weight: 5
routing:
  final: auto
  geosite:
    path: "geosite.dat"
    auto_update: true
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
timeouts:
  dial: 10s
  handshake: 10s
  http_exchange: 60s
  tunnel_idle: 5m
check_interval: 30
max_connections: 1024
```

- `listen`：监听地址，例如 `127.0.0.1:8891`。
- `users`：用户名密码映射。空对象或省略表示匿名。
- `upstreams`：上游代理列表。
  - `url`：支持 `http://`、`https://`、`socks5://`、`socks5h://`。
  - `types`：显式指定上游支持的协议类型，省略时使用 `url` 的 scheme。
  - `priority`：数值越小优先级越高。
  - `weight`：同优先级下的权重，越大被选中的概率越高。
- `routing`：路由规则与分流策略配置。
- `timeouts`：超时配置。
  - `dial`：拨号超时。
  - `handshake`：协议握手超时。
  - `http_exchange`：普通 HTTP 请求交换超时。
  - `tunnel_idle`：隧道空闲超时。
- `check_interval`：健康检查周期，单位秒，默认 30；设置为 0 禁用健康检查。
- `max_connections`：最大并发连接数，默认 1024；设置为负数会拒绝启动。

## 核心逻辑

- **统一端口**：监听单个 TCP 端口，首字节为 `0x05` 时识别为 SOCKS5，否则按 HTTP 处理。
- **级联选择**：
  - `UpstreamManager.Select()` 按优先级分组，同优先级加权轮询选择可用上游。
  - `UpstreamManager.Dial(target)` 在单个上游 Dial 失败时临时标记为 dead，并尝试下一个；全部失败后回退到自身直连。
- **HTTP 代理**：
  - `CONNECT` 方法建立隧道，用于 HTTPS。
  - 普通 HTTP 请求支持上游转发，失败时同样会回退直连。
  - 普通 HTTP 请求会缓冲最多 32MiB 请求体以支持上游失败重试。
- **SOCKS5 代理**：仅实现 `CONNECT` 命令。
- **SOCKS5 域名嗅探**：当客户端发送 IP 目标且 `socks5h_redirect` 开启时，先回复连接成功，再嗅探客户端首包 TLS SNI 或 HTTP Host，恢复域名后重新按域名路由。
- **路由规则**：支持 `domain`、`domain_suffix`、`domain_keyword`、`domain_regex`、`ip_cidr`、`geosite`、`match` 等类型。
- **Auto 缓存**：`auto` 模式会缓存站点的直连或具体上游结果，下次访问优先复用。

## 常见任务

- 修改默认端口：改 `config.yml` 中的 `listen`。
- 添加上游：在 `config.yml` 的 `upstreams` 列表中增加条目。
- 切换上游协议：修改 `url` 的 scheme，或在 `types` 中显式指定。
- 调试：查看日志中的 `upstream ... dead` / `upstream ... recovered`。

## 测试注意事项

- 上游不可达时，代理会自动回退到自身直连。
- 通过 HTTP/HTTPS 上游建立 CONNECT 隧道时，仅读取响应头，不再消费隧道数据，避免隧道建立后阻塞。
- 普通 HTTP 请求会缓冲最多 32MiB 请求体以支持上游失败重试，超限返回 413。
- 健康检查为简单 TCP 拨号，频率由 `check_interval` 控制。
- 监听地址变更后需要重启服务才能生效。

## 已知限制

- 仅支持 SOCKS5 `CONNECT` 命令，不支持 BIND / UDP ASSOCIATE。
- 普通 HTTP 请求体不支持 chunked 编码。
- 健康检查只验证 TCP 可达性，不验证代理协议是否真正可用。
- 监听地址变更后需要重启服务才能生效。
