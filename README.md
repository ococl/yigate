# k5pw 简易透明代理

一个使用 Go 标准库 + `gopkg.in/yaml.v3` 实现的简易代理服务器，支持 HTTP、HTTPS（CONNECT 隧道）和 SOCKS5 三种协议共用同一个端口，支持级联、多上游、优先级、加权负载均衡、规则分流、GEOSITE 以及 auto 缓存。

## 功能

- **统一端口**：HTTP/HTTPS/SOCKS5 共用一个监听端口，自动识别协议。
- **协议支持**：普通 HTTP 请求转发、HTTPS CONNECT 隧道、SOCKS5 CONNECT。
- **级联与多上游**：可配置多个上级代理，支持 HTTP、HTTPS、SOCKS5 三种上游。
- **规则分流**：支持 `direct`、`proxy`、`auto` 三种动作，可按域名、域名后缀、关键词、正则、IP CIDR、GEOSITE 匹配。
- **GEOSITE**：可直接读取并自动更新 v2ray/xray/sing-box 生态的 `geosite.dat`，例如使用 `GEOSITE:CN` 让国内站点直连。
- **Socks5h 重定向**：可开启全量下游请求优先走 SOCKS5 上游远端解析，失败时自动回退原有分流。
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
    types: ["socks5", "socks5h"]
    priority: 1
    weight: 5
  - name: "sg"
    url: "socks5://user:pass@192.168.1.11:1080"
    # 未写 types 时，启动后会异步探测并缓存结果。
    priority: 1
    weight: 3

upstream_type_probe:
  enabled: true
  delay: 30s
  cache_path: "upstream-types.txt"

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

check_interval: 30
max_connections: 1024
```

## 配置说明

- `listen`：监听地址，例如 `127.0.0.1:8891`；如需对外提供服务，可绑定 `0.0.0.0:8891`。
- `users`：用户名密码映射。留空或删除表示匿名。
- `upstreams`：上游代理列表。
  - `name`：上游名称，可被路由规则的 `upstreams` 引用；不写会自动生成 `upstream-1`、`upstream-2`。
  - `url`：上游地址，认证信息可写在 URL 中；未配置 `types` 时会先按 URL scheme 作为初始类型，再由异步探测修正。
  - `types`：可选的代理类型数组，支持 `http`、`https`、`socks5`、`socks5h` 任意组合；已明确指定时不会探测该上游。
  - `priority`：数值越小优先级越高。
  - `weight`：同优先级下的权重，越大被选中的概率越高。
- `upstream_type_probe.enabled`：是否启用上游类型异步探测，默认启用。
- `upstream_type_probe.delay`：启动后等待多久再探测，默认 `30s`。
- `upstream_type_probe.cache_path`：探测结果缓存文件，默认 `upstream-types.txt`；若缓存命中且 URL 一致，下次启动直接复用，不再探测。
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

开启 `auto_update` 后，程序会在启动时先尝试更新 `geosite.dat`，随后按 `update_interval` 定时更新。更新请求会优先使用当前上游代理列表的 auto 选择；如果没有配置上游才会直连下载。`update_url` 可以自定义为内网镜像或其他可访问地址。

## 上游类型探测

如果 `upstreams[].types` 已明确配置，k5pw 会直接使用该类型数组，不再探测该上游。适合已知多协议入口，例如：

```yaml
upstreams:
  - name: "multi"
    url: "http://127.0.0.1:7890"
    types: ["http", "socks5", "socks5h"]
```

未指定 `types` 的上游会先按 URL scheme 作为初始类型，服务启动并等待 `upstream_type_probe.delay` 后异步探测 HTTP/HTTPS/SOCKS5/SOCKS5H 能力；结果写入 `upstream_type_probe.cache_path`，方便用户查看，也方便下次启动在 URL 一致时直接复用并跳过探测。

## Socks5h 重定向

`socks5h_redirect.enabled` 开启后，k5pw 不会改变客户端使用的 HTTP/SOCKS5 协议，而是在内部优先使用 SOCKS5/socks5h 上游的域名地址类型转发目标连接，效果等价于客户端直接使用 `socks5h`：目标域名由上游侧解析，减少本机 DNS 解析。

该模式会缓存每个目标站点成功使用的 SOCKS5 上游；缓存命中时直接复用该上游。如果所有 SOCKS5/socks5h 上游不可用，或本次连接失败，会自动回退到原有规则分流逻辑。

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
