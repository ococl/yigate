# AGENTS.md

## 项目概述

`k5pw` 是一个使用 Go 标准库 + `gopkg.in/yaml.v3` 实现的简易透明代理。支持 HTTP、HTTPS（CONNECT 隧道）和 SOCKS5 三种协议共用一个端口，支持级联、多上游、优先级、加权负载均衡以及回退直连。

## 技术栈

- Go 1.21+
- 依赖：`gopkg.in/yaml.v3`

## 目录结构

```
/export/app/k5pw/
├── main.go        # 入口，加载配置并启动服务
├── config.go      # YAML 配置加载
├── server.go      # 统一 TCP 监听与协议识别
├── go.mod
├── go.sum
├── config.yml     # 运行时配置文件
├── README.md      # 用户文档
└── proxy/
    ├── auth.go       # HTTP Basic / SOCKS5 用户名密码认证
    ├── upstream.go   # 上游管理、优先级、加权轮询、健康检查、级联 Dial
    ├── http.go       # HTTP/HTTPS 代理处理
    └── socks.go      # SOCKS5 代理处理
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
    priority: 1
    weight: 5
check_interval: 30
max_connections: 1024
```

- `listen`：监听地址，例如 `127.0.0.1:8891`。
- `users`：用户名密码映射。空对象或省略表示匿名。
- `upstreams`：上游代理列表。
  - `url`：支持 `http://`、`https://`、`socks5://`。
  - `priority`：数值越小优先级越高。
  - `weight`：同优先级下的权重，越大被选中的概率越高。
- `check_interval`：健康检查周期，单位秒，默认 30。
- `max_connections`：最大并发连接数，默认 1024；设置为负数会拒绝启动。

## 核心逻辑

- **统一端口**：监听单个 TCP 端口，首字节为 `0x05` 时识别为 SOCKS5，否则按 HTTP 处理。
- **级联选择**：
  - `UpstreamManager.Select()` 按优先级分组，同优先级加权轮询选择可用上游。
  - `UpstreamManager.Dial(target)` 在单个上游 Dial 失败时临时标记为 dead，并尝试下一个；全部失败后回退到自身直连。
- **HTTP 代理**：
  - `CONNECT` 方法建立隧道，用于 HTTPS。
  - 普通 HTTP 请求支持上游转发，失败时同样会回退直连。
- **SOCKS5 代理**：仅实现 `CONNECT` 命令。

## 常见任务

- 修改默认端口：改 `config.go` 中的默认值或 `config.yml` 中的 `listen`。
- 添加上游：在 `config.yml` 的 `upstreams` 列表中增加条目。
- 切换上游协议：修改 `url` 的 scheme（如 `http://` 与 `socks5://`）。
- 调试：查看日志中的 `upstream ... dead` / `upstream ... recovered`。

## 测试注意事项

- 上游不可达时，代理会自动回退到自身直连。
- 通过 HTTP/HTTPS 上游建立 CONNECT 隧道时，仅读取响应头，不再消费隧道数据，避免隧道建立后阻塞。
- 普通 HTTP 请求会缓冲最多 32MiB 请求体以支持上游失败重试，超限返回 413。
- 健康检查为简单 TCP 拨号，频率由 `check_interval` 控制。

## 已知限制

- 仅支持 SOCKS5 `CONNECT` 命令，不支持 BIND / UDP ASSOCIATE。
- 普通 HTTP 请求体不支持 chunked 编码。
- 健康检查只验证 TCP 可达性，不验证代理协议是否真正可用。
