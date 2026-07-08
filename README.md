# k5pw 简易透明代理

一个使用 Go 标准库 + `gopkg.in/yaml.v3` 实现的简易代理服务器，支持 HTTP、HTTPS（CONNECT 隧道）和 SOCKS5 三种协议共用同一个端口，支持级联、多上游、优先级、加权负载均衡以及回退直连。

## 功能

- **统一端口**：HTTP/HTTPS/SOCKS5 共用一个监听端口，自动识别协议。
- **协议支持**：
  - HTTP 代理：普通 HTTP 请求转发、HTTPS CONNECT 隧道。
  - HTTPS 代理：通过 CONNECT 建立 TLS 隧道。
  - SOCKS5 代理：支持 CONNECT 命令。
- **级联与多上游**：可配置多个上级代理，支持 HTTP、HTTPS、SOCKS5 三种上游。
- **优先级**：优先使用 `priority` 值更小的上游组，组内按权重轮询。
- **加权负载均衡**：同优先级内使用平滑加权轮询。
- **回退自身**：所有上级不可用时自动回退到本机直连。
- **账号密码**：默认匿名，配置 `users` 后启用 HTTP Basic 认证与 SOCKS5 用户名密码认证。
- **健康检查**：定时探测上游可用性，故障自动剔除，恢复自动加入。

## 快速开始

```bash
cd /export/app/k5pw
go build -o k5pw
./k5pw -config config.yml
```

默认监听 `127.0.0.1:8891`，可修改 `config.yml` 调整配置。

## 配置说明

```yaml
listen: "127.0.0.1:8891"
users:
  admin: "admin123"
upstreams:
  - url: "http://10.2.3.150:7897"
    priority: 1
    weight: 5
  - url: "socks5://user:pass@192.168.1.11:1080"
    priority: 1
    weight: 3
  - url: "https://backup.example.com:443"
    priority: 2
    weight: 1
check_interval: 30
max_connections: 1024
```

- `listen`：监听地址，例如 `127.0.0.1:8891`；如需对外提供服务，可绑定 `0.0.0.0:8891` 或公网地址。
- `users`：用户名密码映射。留空或删除表示匿名。
- `upstreams`：上游代理列表。
  - `url`：支持 `http://`、`https://`、`socks5://`。
  - `priority`：数值越小优先级越高。
  - `weight`：同优先级下的权重，越大被选中的概率越高。
- `check_interval`：健康检查周期，单位秒，默认 30。
- `max_connections`：最大并发连接数，默认 1024；设置为负数会拒绝启动。

## 使用示例

### HTTP 代理

```bash
curl -x http://admin:admin123@127.0.0.1:8891 https://www.example.com
```

### SOCKS5 代理

```bash
curl -x socks5://admin:admin123@127.0.0.1:8891 https://www.example.com
```

## 级联说明

- 本代理会优先选择 `priority` 最小的可用上游组。
- 同优先级组内按 `weight` 平滑加权轮询。
- 当所有上游都不可用时，自动回退到本机直接连接目标。
- 上游认证信息可直接写在 URL 中，例如 `http://user:pass@host:port`。

## 注意

- 本项目仅用于学习、测试或内部网络管理，请勿用于非法用途。
- 仅实现了 SOCKS5 的 CONNECT 命令，暂不支持 BIND 与 UDP ASSOCIATE。
- 普通 HTTP 请求会缓冲最多 32MiB 请求体以支持上游失败重试，超限返回 413。
