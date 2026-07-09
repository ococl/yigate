# k5pw

`k5pw` 是一个轻量级 Go 代理服务器，支持 HTTP、HTTPS CONNECT 与 SOCKS5 共用同一个监听端口。项目提供多上游级联、规则分流、GEOSITE、socks5h 远端解析、配置热加载、管理接口、Prometheus 指标和结构化日志。

## 主要特性

- HTTP、HTTPS CONNECT、SOCKS5 统一端口接入。
- SOCKS5 支持 CONNECT、BIND、UDP ASSOCIATE。
- 支持 HTTP、HTTPS、SOCKS5、SOCKS5H 上游代理。
- 多上游支持优先级与加权轮询。
- 支持 direct、proxy、auto 三种路由动作。
- 支持域名、后缀、关键词、正则、IP CIDR、GEOSITE 与 match 规则。
- 支持 `geosite.dat` 自动更新。
- 支持 socks5h 重定向与 TLS SNI / HTTP Host 目标恢复。
- 支持配置文件保存后自动热加载。
- 支持 IP 白名单、单 IP 速率限制、单用户并发限制。
- 支持 `/health`、`/metrics`、`/api/v1/status`、`/api/v1/reload` 管理接口。
- 支持可选 `/debug/pprof` 调试端点。
- 支持 JSON / console 日志、文件输出和日志滚动。
- 支持 Linux systemd 服务和 Docker 运行。

## 目录结构

```text
.
├── src/                 # Go 源码、默认配置、Dockerfile
│   ├── main.go          # CLI 入口与服务命令
│   ├── config.go        # 配置加载、默认值与校验
│   ├── server.go        # TCP 监听、协议识别、热加载
│   ├── service.go       # 系统服务集成
│   └── proxy/           # 协议处理、路由、上游、管理接口
├── dist/                # Docker/systemd 分发配置
├── scripts/             # 发布脚本
└── pyscripts/           # 代理联调脚本
```

## 环境要求

- Go 1.21+
- Linux/macOS/Windows 均可构建；systemd 安装脚本面向 Linux。
- Docker 运行需要 Docker Compose。

## 快速开始

```bash
cd src
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o k5pw .
./k5pw -config config.sample.yml run
```

默认命令为 `run`，也可以省略：

```bash
./k5pw -config config.sample.yml
```

查看版本：

```bash
./k5pw -version
```

## 常用命令

```bash
# 前台运行
./k5pw -config config.yml run

# 安装为系统服务
sudo ./k5pw -config /etc/k5pw/config.yml install

# 启停系统服务
sudo ./k5pw -config /etc/k5pw/config.yml start
sudo ./k5pw -config /etc/k5pw/config.yml stop
sudo ./k5pw -config /etc/k5pw/config.yml restart

# 卸载系统服务
sudo ./k5pw -config /etc/k5pw/config.yml uninstall
```

## 配置说明

参考 `src/config.sample.yml`。核心字段如下：

- `listen`：代理监听地址。
- `management`：管理接口监听地址；为空则不启用。
- `management_auth_token`：管理接口 Bearer Token；管理端口绑定非回环地址时必须配置。
- `users`：下游代理认证用户表；为空表示允许匿名访问。
- `upstreams`：上游代理列表，支持 `http`、`https`、`socks5`、`socks5h`。
- `routing.final`：未命中规则时的默认动作，支持 `direct`、`proxy`、`auto`。
- `routing.rules`：按顺序匹配的路由规则。
- `routing.geosite`：`geosite.dat` 路径与自动更新配置。
- `routing.auto`：自动模式策略、直连探测超时和缓存时间。
- `routing.socks5h_redirect`：优先使用 SOCKS5H 上游进行远端解析。
- `access_control`：客户端白名单、速率限制和用户并发限制。
- `timeouts`：拨号、握手、HTTP 交换和隧道空闲超时。
- `log`：日志级别、格式、输出位置和滚动策略。
- `check_interval`：上游健康检查间隔，单位秒。
- `max_connections`：全局最大下游连接数；为 0 表示不限制。
- `enable_pprof`：是否启用 pprof 管理端点。

配置中的密码、Token、上游地址可使用环境变量，例如 `${K5PW_PROXY_PASSWORD}`。引用的环境变量未设置或为空时，配置加载会失败。

## 管理接口

启用 `management` 后可访问：

- `GET /health`：健康检查。
- `GET /metrics`：Prometheus 指标。
- `GET /api/v1/status`：运行状态摘要。
- `POST /api/v1/reload`：手动重新加载配置。
- `/debug/pprof/*`：当 `enable_pprof: true` 时启用。

配置了 `management_auth_token` 时，请携带请求头：

```text
Authorization: Bearer <token>
```

## Docker 运行

```bash
cd dist
docker compose up --build
```

`dist/docker-compose.yml` 使用 `../src/Dockerfile` 构建镜像，并将 `dist/k5` 挂载到容器内 `/k5`。

## 测试

```bash
cd src
GOPROXY=https://goproxy.cn,direct go test ./...
```

代理联调脚本位于 `pyscripts/`，运行前需要先启动对应端口的代理服务。

## 发布构建

```bash
./scripts/release.sh 1.2.0
```

脚本会生成多平台二进制到 `build/`，并创建对应 Git tag。

## 安全建议

- 对公网开放代理端口前务必配置强密码和客户端白名单。
- 管理端口如绑定非回环地址必须配置 `management_auth_token`。
- 不要把真实密码、Token、上游账号写入仓库；优先使用环境变量。
- `enable_pprof` 仅建议在排障时临时开启。
