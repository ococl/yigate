# k5pw 简易透明代理

一个使用 Go 标准库 + `gopkg.in/yaml.v3` 实现的简易代理服务器，支持 HTTP、HTTPS（CONNECT 隧道）和 SOCKS5 三种协议共用同一个端口，支持级联、多上游、优先级、加权负载均衡、规则分流、GEOSITE 以及 auto 缓存。

## 功能

- **统一端口**：HTTP/HTTPS/SOCKS5 共用一个监听端口，自动识别协议。
- **协议支持**：普通 HTTP 请求转发、HTTPS CONNECT 隧道、SOCKS5 CONNECT。
- **级联与多上游**：可配置多个上级代理，支持 HTTP、HTTPS、SOCKS5 三种上游。
- **规则分流**：支持 `direct`、`proxy`、`auto` 三种动作，可按域名、域名后缀、关键词、正则、IP CIDR、GEOSITE 匹配。
- **GEOSITE**：可直接读取 v2ray/xray/sing-box 生态的 `geosite.dat`，例如使用 `GEOSITE:CN` 让国内站点直连。
- **Auto 缓存**：auto 模式会缓存站点的直连或具体上游结果，下次访问优先复用。
- **优先级与权重**：同一批候选上游内，优先使用 `priority` 更小的组，组内按 `weight` 平滑加权轮询。
- **账号密码**：默认匿名，配置 `users` 后启用 HTTP Basic 认证与 SOCKS5 用户名密码认证。
- **健康检查**：定时探测上游可用性，故障自动剔除，恢复自动加入。

## 快速开始

```bash
cd /export/app/k5pw
go build -o k5pw
./k5pw -config config.yml
```

默认监听 `127.0.0.1:8891`，可修改 `config.yml` 调整配置。

## 配置示例

```yaml
listen: "0.0.0.0:8891"
users:
  admin: "admin123"

upstreams:
  - name: "hk"
    url: "socks5://10.2.3.150:7897"
    priority: 1
    weight: 5
  - name: "sg"
    url: "socks5://user:pass@192.168.1.11:1080"
    priority: 1
    weight: 3

routing:
  final: auto
  geosite:
    path: "geosite.dat"
  auto:
    mode: direct_first
    direct_timeout: 1500ms
    cache_ttl: 1h
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

check_interval: 30
max_connections: 1024
```

## 配置说明

- `listen`：监听地址，例如 `127.0.0.1:8891`；如需对外提供服务，可绑定 `0.0.0.0:8891`。
- `users`：用户名密码映射。留空或删除表示匿名。
- `upstreams`：上游代理列表。
  - `name`：上游名称，可被路由规则的 `upstreams` 引用；不写会自动生成 `upstream-1`、`upstream-2`。
  - `url`：支持 `http://`、`https://`、`socks5://`，认证信息可写在 URL 中。
  - `priority`：数值越小优先级越高。
  - `weight`：同优先级下的权重，越大被选中的概率越高。
- `routing.final`：所有规则都不匹配时的动作，支持 `direct`、`proxy`、`auto`，默认 `auto`。
- `routing.geosite.path`：`geosite.dat` 路径。相对路径会按配置文件所在目录解析。
- `routing.auto.mode`：`direct_first` 或 `proxy_first`，默认 `direct_first`。
- `routing.auto.direct_timeout`：auto 直连快速探测超时，例如 `1500ms`、`2s`。
- `routing.auto.cache_ttl`：auto 缓存有效期，例如 `1h`。
- `routing.rules`：按顺序匹配，命中第一条即返回。
- `check_interval`：健康检查周期，单位秒，默认 30。
- `max_connections`：最大并发连接数，默认 1024；设置为负数会拒绝启动。

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

## 使用示例

### HTTP 代理

```bash
curl -x http://admin:admin123@127.0.0.1:8891 https://www.example.com
```

### SOCKS5 代理

```bash
curl -x socks5://admin:admin123@127.0.0.1:8891 https://www.example.com
```

## 注意

- 本项目仅用于学习、测试或内部网络管理，请勿用于非法用途。
- 仅实现了 SOCKS5 的 CONNECT 命令，暂不支持 BIND 与 UDP ASSOCIATE。
- 普通 HTTP 请求会缓冲最多 32MiB 请求体以支持重试，超限返回 413。
- `GEOSITE:CN` 是域名分流，不需要提前 DNS 解析；GEOIP 暂未实现。
