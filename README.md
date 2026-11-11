<p align="center">
  <img src="web/public/favicon.svg" alt="YiGate" width="96" height="96">
</p>

<h1 align="center">YiGate</h1>

<p align="center">
  <a href="README.md">简体中文</a> · <a href="README.en.md">English</a>
</p>

<p align="center">
  轻量级统一代理网关。让 <strong>HTTP / HTTPS CONNECT / SOCKS5 共用单端口</strong>接入，并通过规则路由、多上游、健康检查、socks5h 远端解析、配置热加载和内置管理界面，提供可运维的代理能力。
</p>

> 合规、轻量、可运维。明确不支持任何混淆 / 非合规协议。

> **仓库说明**
>
> - **主仓库（唯一开发与发布通道）**：<https://cnb.cool/yi.team/yigate>
> - **GitHub 镜像**：<https://github.com/ococl/yigate>
>
> GitHub 仅作镜像：代码、标签与 Release（含全部附件）由 CNB 流水线在正式发布后自动同步，
> 该仓库**不运行 CI**，也不作为发布来源。Issue、PR 与讨论请提交到主仓库。
> 版本与发布流程见 [VERSIONING.md](VERSIONING.md)，贡献方式见 [CONTRIBUTING.md](CONTRIBUTING.md)。


---

## 特性

- **单端口多协议**：同一监听端口自动识别并分流 HTTP、HTTPS CONNECT、SOCKS5。
- **规则路由**：`domain` / `domain_suffix` / `domain_keyword` / `domain_regex` / `ip_cidr`（IPv4+IPv6）/ `geosite` / `match`；动作支持 `direct`（只直连）、`proxy`（只走上游）、`direct_first`（直连优先）、`proxy_first`（代理优先）、`auto`（沿用全局 `auto.mode`）与 `deny`。
- **多上游与负载**：`weighted_round_robin` / `least_connections` / `consistent_hash` / `source_ip_hash`，含优先级、权重、死节点剔除。
- **健康检查**：TCP / HTTP / HTTPS / TLS 四种探测。
- **DNS 增强**：内置 DoH / DoT 解析器，系统 DNS 兜底，TTL 缓存，IPv4/IPv6 优先级策略。
- **socks5h 远端解析**：上游侧完成域名解析，避免本地 DNS 泄露。
- **透明代理**：仅 Linux，支持 `redirect` / `tproxy` 两种模式（`scripts/` 下含 iptables/nftables 示例）。
- **TLS 终止**：listener 层统一 TLS，支持客户端证书（mTLS）。
- **配置热加载**：文件变更自动重载，监听地址 / TLS / 上游 / 规则可热更新，先关旧 listener 再建新。
- **访问控制**：CIDR 白名单、单 IP 限速、用户并发限制、匿名开关。
- **SSRF 防护**：`destination_policy` 默认拒绝回环 / 私网 / 链路本地 / 云元数据等敏感目标。
- **流量审计与配额**：按 user / upstream / target 三维统计，支持配额限制；流量按自然日持久化，默认保留 30 天并自动清理。
- **可观测性**：`/health`、`/metrics`（Prometheus，`yigate_` 前缀）、`/api/v1/status`、运行时配置 API。
- **内嵌 Web 管理界面**：Vue 3 + antdv-next，随二进制启动，无需额外部署；界面支持简体中文 / English 切换。
- **多形态部署**：原生二进制、Docker、systemd 服务；运行目录布局在二进制与 Docker 间一致。
- **跨平台**：26 个目标平台构建（Linux / macOS / Windows / FreeBSD / OpenBSD / NetBSD，含 arm/mips/riscv/loong64 等）。

---

## 界面预览

管理后台由二进制内嵌，随服务启动即可访问（默认 `http://127.0.0.1:8890`），无需单独部署前端。以下截图取自 `v0.2.0` 演示实例，数据均为示例。

![仪表盘](docs/screenshots/dashboard.png)

| 上游管理 | 路由规则 |
| --- | --- |
| [![上游管理](docs/screenshots/upstreams.png)](docs/screenshots/upstreams.png) | [![路由规则](docs/screenshots/rules.png)](docs/screenshots/rules.png) |
| **用户管理** | **流量审计** |
| [![用户管理](docs/screenshots/users.png)](docs/screenshots/users.png) | [![流量审计](docs/screenshots/traffic.png)](docs/screenshots/traffic.png) |
| **系统设置** | **登录** |
| [![系统设置](docs/screenshots/settings.png)](docs/screenshots/settings.png) | [![登录](docs/screenshots/login.png)](docs/screenshots/login.png) |

---

## 目录结构

```text
.
├── src/                  # Go 后端（module yigate）
│   ├── main.go           # CLI 入口
│   ├── config.go         # 配置加载 / 校验
│   ├── server.go         # 统一 TCP Server / 热加载
│   ├── config.sample.yml # 配置示例
│   └── proxy/            # 代理核心（协议 / 路由 / 上游 / DNS / TPROXY / 管理）
├── web/                  # 前端（Vue3 + Vite + TS + antdv-next），独立构建
├── deploy/               # Docker / systemd / 配置模板
├── scripts/              # 构建 / 发布 / 平台清单 / TPROXY 示例
├── docs/screenshots/     # 界面截图
├── test/e2e/             # 联调与基准脚本
├── .cnb.yml              # CNB 主发布通道
├── .github/workflows/    # GitHub 手动排查入口（不自动触发，构建发布由 CNB 承担）
├── docker-compose.yml    # 一键本地部署
└── Makefile
```

### 运行时目录（原生二进制与 Docker 一致）

```text
.
├── data/
│   ├── config.yml        # 运行配置（默认 -config 路径，首次启动自动生成）
│   ├── traffic.json      # 流量审计持久化数据（自动生成，默认保留 30 天）
│   ├── geosite.dat       # GeoSite 数据
│   └── geosite.dat.meta.json  # GeoSite 版本元数据（自动生成：SHA-256、来源、检查时间）
└── logs/
    └── yigate.log        # 滚动日志
```

---

## 快速开始

### 方式一：原生二进制

```bash
cd src
GOPROXY=https://goproxy.cn,direct go build -o yigate .
./yigate -config data/config.yml run      # 首次运行自动生成 data/config.yml
```

前台运行后访问：

- 代理入口：`http://127.0.0.1:8891`（HTTP/HTTPS/SOCKS5 共用）
- 管理界面：`http://127.0.0.1:8890`
- 健康检查：`curl http://127.0.0.1:8890/health`（匿名，返回纯文本 `UP`；代理是否可用看 `/api/v1/status`）

### 方式二：Docker（推荐）

```bash
docker compose up --build
```

容器将挂载当前目录的 `./data` 与 `./logs`，配置位于 `/var/lib/yigate/data/config.yml`。默认只把代理和管理端口绑定到宿主机回环地址；需要局域网访问时设置 `YIGATE_PROXY_BIND=0.0.0.0`，并先在配置中设置代理用户密码，避免开放匿名代理。

### 方式三：systemd

```bash
sudo bash deploy/systemd/install.sh
sudo systemctl start yigate
```

`install.sh lan` 会生成随机代理密码和管理 Token，写入 `/etc/yigate/initial-credentials`（仅 root 可读）。

---

## 配置要点

完整示例见 `src/config.sample.yml`。关键字段：

| 字段 | 说明 |
|------|------|
| `listen` / `listeners` | 代理监听地址；`listeners` 支持每端口独立 TLS / 用户 / 访问控制，顶层 `users`/`access_control` 作为未显式配置端口的默认值 |
| `management` | 独立管理端口（WebUI + API） |
| `management_auth_token` | 非空时除 `/health` 外的管理接口要求 Bearer Token；非回环暴露时必填 |
| `webui.enabled` | 默认 true；热加载生效，关闭后管理端口仅提供 API |
| `enable_pprof` | 默认 false；开启后管理端口暴露 `/debug/pprof/*`（需认证），可热加载 |
| `users` | 为空则允许匿名代理 |
| `tls` | 服务端终止：`protocol`（`auto`/`gm`/`tls`）、标准 TLS 证书、国密 SM2 双证书、`client_ca_file` |
| `tls.protocol` | 默认 `auto`：国密 TLCP 优先、回落标准 TLS；`gm` 强制国密；`tls` 保持标准 TLS |
| `upstreams` | 上游代理，类型 `http`/`https`/`socks5`/`socks5h`，策略与健康检查；https 上游支持 `tls_protocol`/`tls_ca_file`/`tls_server_name` |
| `routing.rules` / `routing.final` | 规则列表（首个命中生效）与兜底动作 `direct`/`proxy`/`direct_first`/`proxy_first`/`auto`/`deny` |
| `routing.auto.mode` | `direct_first`（默认）或 `proxy_first`；规则上的 `direct_first`/`proxy_first` 会覆盖该全局设置 |
| `routing.socks5h_redirect` | 目标域名交由上游解析（远端 DNS），可用 `upstreams` 限定范围 |
| `routing.geosite` | 数据路径、自动更新地址/间隔、`update_token`（从上游拉取），以及 `sync_enabled`/`sync_token`（供下游只读同步） |
| `dns` | `system` / `doh` / `dot`，`ip_precedence` 控制 IPv4/IPv6 优先；`fallback` 默认 true |
| `destination_policy` | `protect_private`（默认 true）、`default_action` 及域名/端口/CIDR 白黑名单 |
| `timeouts` | `dial` / `handshake` / `http_exchange` / `tunnel_idle`，0 表示使用内置默认值 |
| `log` | 级别、格式（json/console）、输出（stdout/file）与轮转参数 |
| `check_interval` | 健康检查间隔（秒），0 表示停止周期性检查 |
| `max_connections` | listener 未单独设置时的并发上限兜底，0 表示不限制 |
| `traffic_audit.enabled` | 流量审计与配额；`retention_days` 控制明细保留天数（默认 30） |
| `tproxy.enabled` | 透明代理（仅 Linux） |

流量明细保存在配置文件同目录的 `traffic.json`（权限 0600），包含用户名与目标域名，请按敏感数据保护。

配置中 `${ENV_NAME}` 会被严格展开，缺失或空值会导致启动失败。
加载阶段会校验监听地址、TLS 文件、上游 URL/策略/健康检查、访问控制数值与路由枚举，
非法配置直接启动失败而不是静默降级。

---

## 管理接口

| 路径 | 说明 |
|------|------|
| `GET /health` | 进程探活（匿名，返回纯文本 `UP`；不反映代理是否可用） |
| `GET /metrics` | Prometheus 指标（`yigate_` 前缀） |
| `GET /api/v1/status` | 运行摘要（含版本与运行时长） |
| `GET /api/v1/version` | 版本信息 |
| `POST /api/v1/reload` | 手动重载配置 |
| `GET /api/v1/traffic` | 流量统计快照（`enabled` 表示审计开关） |
| `GET/POST /api/v1/config/upstreams` | 列出 / 添加上游（含健康检查配置与 `enabled` 开关） |
| `PUT/DELETE /api/v1/config/upstreams/{name}` | 更新 / 删除上游（局部更新，未提交字段保持原值；`enabled=false` 暂停调度与健康检查） |
| `GET/POST /api/v1/config/rules` | 列出 / 添加路由规则 |
| `PUT /api/v1/config/rules` | 整体替换路由规则（一次校验、一次落盘、一次热加载） |
| `POST /api/v1/config/rules/batch-toggle` | 批量启用/禁用规则（一次落盘） |
| `PUT /api/v1/config/rules/{index}` | 更新路由规则（含 `disabled` 开关） |
| `POST /api/v1/config/rules/{index}/move` | 上移 / 下移路由规则 |
| `DELETE /api/v1/config/rules/{index}` | 删除路由规则 |
| `GET/POST /api/v1/config/users` | 列出 / 添加用户 |
| `PUT /api/v1/config/users/{username}` | 修改用户密码 |
| `DELETE /api/v1/config/users/{username}` | 删除用户 |
| `GET/PUT /api/v1/config/settings` | 读取 / 部分更新全局设置（持久化 + 热加载） |
| `GET /api/v1/geosite` | GeoSite 版本、大小、来源与同步配置 |
| `POST /api/v1/geosite/update` | 立即更新 GeoSite 数据（仅管理令牌） |
| `GET /api/v1/geosite/dat` | 下载 GeoSite 数据（支持 ETag/304，可用只读同步令牌） |

配置写入前会做校验，写入后若热加载失败会自动回滚配置文件，避免残留无法启动的配置。

### GeoSite 更新与下游同步

WebUI 的「GeoSite 数据」页面可查看版本（SHA-256 前 12 位）、文件大小、数据更新时间、来源地址与上次检查时间，并支持手动更新。数据文件固定保存在配置文件同目录的 `geosite.dat`，由系统托管，无需也不允许在界面指定路径。更新时优先使用已配置且可用的上游代理，全部失败后回退直连；可通过 `use_upstream` 开关关闭。

多节点部署时可由一个节点统一更新，其余节点只读同步。同步令牌由系统随机生成，在 WebUI 中默认掩码显示并可直接复制；勾选「允许下游同步」时若令牌为空会自动生成，也可一键「重新生成」（或调用 `GET`/`POST /api/v1/geosite/sync-token`），无需手写：

```yaml
# 上游节点：对外提供只读同步（sync_token 开启后自动生成并写回配置）
routing:
  geosite:
    auto_update: true
    update_url: "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
    update_interval: 24h
    use_upstream: true
    sync_enabled: true

# 下游节点：从上游节点同步
routing:
  geosite:
    auto_update: true
    update_url: "http://node-a:8892/api/v1/geosite/dat"
    update_interval: 24h
    use_upstream: true
    update_token: "<上游 WebUI 生成的同步令牌>"
```

同步流程带条件请求：本节点响应 `ETag`（内容 SHA-256）与 `X-GeoSite-Version`，下游携带 `If-None-Match`，数据未变化时返回 `304` 且不重写文件、不触发重载。内容变化时原子替换 `geosite.dat` 并写入 `geosite.dat.meta.json`，随后热重载路由，无需重启。

`/debug/pprof/*` 仅在 `enable_pprof: true` 时启用（敏感入口，勿公网暴露）。

---

## 构建与开发

```bash
# Go 后端
cd src && GOPROXY=https://goproxy.cn,direct go build -o yigate .

# 前端（独立项目，Node 26.13，见 .nvmrc）
cd web && npm ci && npm run build
# 构建后通过 embed-dist.mjs 压缩并嵌入 WebUI，再重新编译 Go
# （直接 make build 会自动完成这两步）

# 全量构建 / 发布
make build
./scripts/release.sh 1.2.0
```

测试：

```bash
# Go 全量测试与竞态检测
cd src && GOPROXY=https://goproxy.cn,direct go test ./...
cd src && GOPROXY=https://goproxy.cn,direct go test -race ./...

# 前端单元/API 契约测试
make web-test

# 真实浏览器 E2E（首次需安装 Chromium）
make web-e2e-install
make web-e2e
```

`release.sh` 只负责发布门禁与 WebUI 构建，跨平台编译、打包、SBOM 和校验和统一由
`scripts/ci-build.sh` 生成，避免 CI 与正式发布出现两套制品流程。

---

## 国密（TLCP）适配

传输层国密基于 GB/T 38636-2020 实现，纯 Go、`CGO_ENABLED=0` 可用，覆盖 26 个发布平台。

**能力范围**

| 位置 | 能力 |
|------|------|
| 入站监听 | `protocol: gm` 强制 TLCP（SM2 双证书）；`auto` 在同一端口自适应 TLCP / 标准 TLS；`tls` 行为不变 |
| 出站 https 上游 | `tls_protocol: gm` 强制国密，`auto` 国密优先并记忆协商结果，`tls` 标准 TLS |
| 健康检查 | `https`/`tls` 探测跟随上游协议，避免国密上游被误判为不可用 |
| 摘要 | 令牌比较摘要使用 SM3（合规口径，不改变安全强度） |

**性能基线**

完整握手与单连接吞吐（`./scripts/bench-tls.sh` 复现）：

| 场景 | 完整握手 | 吞吐（32 KB 回环 echo） |
|------|---------|----------------------|
| 标准 TLS / RSA-2048 | 2.90 ms | 417 MB/s |
| 标准 TLS / ECDSA P-256 | 1.30 ms | — |
| TLCP / SM2 双证书 | 0.89 ms | 89 MB/s |
| `auto` 端口（TLCP 客户端） | 0.93 ms | — |

参考环境：2 vCPU Skylake（无 SM4 硬件指令）、Go 1.27、`CGO_ENABLED=0`、回环网络。

读法：

- TLCP 握手更快是因为它基于 TLS 1.1 风格协议、握手消息更少，**不能据此得出"国密更快/更安全"的结论**；与 ECDSA P-256 对照才反映 SM2 自身的签名开销。
- 数据面差距来自 SM4 软件实现；CPU 带 SM4 指令集（ARMv8.4-SM4 等）时差距会明显收窄。
- `auto` 端口相比专用端口约有 5% 的握手开销（需要探测 ClientHello），稳态连接不受影响。
- 数字随 CPU、内核与编译选项变化，密评现场必须在同一台机器上复测。

**生成样板证书**

```bash
# 生成 SM2 根 CA + 签名证书 + 加密证书（-p 输出 PBES2-SM4 加密私钥）
./yigate gencert -o certs/gm -d proxy.example.com

# 复用已有 CA 签发（轮换证书时不必重建信任链，不会覆盖原 ca.crt/ca.key）
./yigate gencert -o certs/gm-gw -d gw.example.com \
  -ca certs/gm/ca.crt -ca-key certs/gm/ca.key \
  -ca-key-password '${YIGATE_GM_CA_KEY_PASSWORD}'
```

`-ca` / `-ca-key` 必须成对提供；CA 私钥支持 PBES2-SM4 加密，口令写 `${VAR}` 时按环境变量展开，
其余情况按字面值处理。签发前会校验证书与私钥是否配对，不匹配或口令错误直接报错。

**有效期与口令**

- 叶子证书默认 365 天，用 `-days` 调整；自带 CA 固定 10 年（仅作样板）。
- 请求的有效期超过签发 CA 的剩余有效期时，自动收敛到 CA 的到期时间并打印提示。
  签出比 CA 活得久的证书没有意义——CA 一到期，整条链就不可信。
- CA 已过期时直接报错，不会签出当场失效的证书。
- 私钥口令无法原地更换。轮换叶子证书口令：用相同 `-ca` 重新签发一套（`-p` 填新口令），
  同步更新 `${YIGATE_GM_SIGN_KEY_PASSWORD}` / `${YIGATE_GM_ENC_KEY_PASSWORD}` 后滚动重启。
  CA 私钥口令目前不支持重封装，更换需重建 CA，并同步替换所有下游的信任锚。

```bash
# 指定叶子证书有效期（示例：半年一换）
./yigate gencert -o certs/gm-gw -d gw.example.com -days 180 \
  -ca certs/gm/ca.crt -ca-key certs/gm/ca.key
```

**到期提醒**

进程启动时和每次配置热加载后都会扫描配置中引用的证书（监听证书、国密双证书、`client_ca_file`、
上游 `tls_ca_file`）：剩余不足 30 天打 `certificate expires soon` 警告，已过期打
`certificate has expired` 错误。日志里带文件路径、证书类型、CN 与剩余天数。

检查失败不阻断启动——真正不可用的证书会在建监听或握手时以更明确的错误暴露。
同一个文件被多个监听共用时只报一次。

WebUI 顶部会据此显示到期横幅；`GET /api/v1/status` 的 `certificates` 字段给出同样的数据，
可直接接监控告警。上游表单里的 `tls_ca_file` 支持从配置目录列出的证书文件中选择，
不用手抄路径。

不想启动服务也能体检：`-check` 扫描文件或目录（不递归），打印 CN、有效期与剩余天数，
有证书过期或落在 30 天窗口内时退出码为 1，可直接用在发布脚本里拦截。

```bash
./yigate gencert -check certs/gm
# FILE               KIND  SUBJECT         NOT BEFORE  NOT AFTER   REMAINING  STATUS
# certs/gm/ca.crt    ca    yigate GM CA    2026-11-11  2036-11-08  3649 days  ok
# certs/gm/enc.crt   leaf  gw.example.com  2026-11-11  2026-12-01  19 days    expiring
```

**入站配置**

```yaml
listeners:
  - listen: "0.0.0.0:8443"
    tls:
      protocol: gm
      gm_sign_cert_file: "certs/gm/sign.crt"
      gm_sign_key_file: "certs/gm/sign.key"
      gm_enc_cert_file: "certs/gm/enc.crt"
      gm_enc_key_file: "certs/gm/enc.key"
      # gm_sign_key_password: "${YIGATE_GM_SIGN_KEY_PASSWORD}"
      # gm_enc_key_password: "${YIGATE_GM_ENC_KEY_PASSWORD}"
      cipher_suites: ["ECDHE_SM4_GCM_SM3", "ECC_SM4_GCM_SM3"]
```

**出站配置**

```yaml
upstreams:
  - name: "gm-gateway"
    url: "https://gw.example.com:8443"
    tls_protocol: "gm"            # auto 时国密优先、失败回落标准 TLS
    tls_ca_file: "certs/gm/ca.crt"
    tls_server_name: "gw.example.com"
```

**边界说明（二次开发请从这里接手）**

- 标准 TLS 与 TLCP 的密钥材料不能复用：TLCP 要求 SM2 双证书，Go 标准库不支持 SM2 曲线，因此 `cert_file` 始终表示标准 TLS 证书。
- 管理端口（WebUI/API）不提供国密 TLS，浏览器普遍不支持 TLCP；如确需国密管理面，应在前置国密网关卸载。
- DNS over TLS / HTTPS 保持标准协议：公共 DNS 服务不使用国密套件，改造点在服务端而非本程序。
- WebUI 上游编辑不会覆盖 `tls_protocol` 等字段（未提交即保留原值）；国密相关字段建议直接在配置文件中维护。

**密评自查**

按测评口径逐项核对（实现现状、配置项、取证命令、已知边界）见
[docs/gm-compliance.md](docs/gm-compliance.md)。其中最容易踩的一条：要求"强制国密"时必须用
`protocol: gm`，`auto` 允许回落标准 TLS。
- `auto` 端口启用 mTLS 时，`client_ca_file` 需同时包含标准 TLS CA 与 SM2 CA（可把多个 PEM 块拼在一个文件里），
  两侧各取所需；只放 SM2 CA 会导致标准 TLS 侧启动失败并给出明确提示。
- 代理协议自身（HTTP Basic、SOCKS5 RFC1929）与 CONNECT 隧道内到目标站点的加密不由本程序决定，无法国密化。
- 未内置密码机 / KMS / 密钥轮换，私钥保护仅提供 PBES2-SM4 口令加密；密评场景需补齐密码模块资质与密钥管理。
- `auto` 出站首次连接会先尝试 TLCP，对不支持国密的上游多一次握手开销，协商结果按上游记忆并在 5 分钟后重试，不影响稳态延迟。

---

## 安全与合规

YiGate 仅用于合法、合规的代理场景。项目明确 **不支持任何混淆协议、隧道伪装或非合规传输**，不接受相关功能请求。管理接口与 pprof 属于敏感入口，应通过 Token 与网络隔离保护。部署安全基线见 [SECURITY.md](SECURITY.md)；发现漏洞请走私有披露渠道，不要在公开 Issue 中提交。

详见 [FEATURES.md](FEATURES.md)、[VERSIONING.md](VERSIONING.md)、[CONTRIBUTING.md](CONTRIBUTING.md)、[SECURITY.md](SECURITY.md) 与 [CHANGELOG.md](CHANGELOG.md)。

## 许可证

见 [LICENSE](LICENSE)。
