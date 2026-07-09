# k5pw 故障排查手册

本文档覆盖 k5pw 生产环境中常见问题的排查思路与操作命令。

---

## 1. 代理无法启动

### 现象

执行 `k5pw -config config.yml` 后立即退出，终端打印错误。

### 排查步骤

1. **检查配置文件语法**

   ```bash
   cd src
   go run . -config config.yml
   ```

   如果 YAML 缩进错误，会提示 `yaml: line N:` 类信息。

2. **检查 `listen` 地址是否合法**

   ```bash
   python3 -c "import socket; socket.getaddrinfo('127.0.0.1', 8891)"
   ```

   必须能被 `net.ResolveTCPAddr("tcp", addr)` 解析。

3. **检查端口是否被占用**

   ```bash
   ss -ltnp | grep 8891
   ```

4. **检查 `geosite.dat` 路径**

   若配置了 `routing.geosite.path` 且文件不存在，程序会拒绝启动。相对路径按**配置文件所在目录**解析。

   ```bash
   ls -l $(dirname config.yml)/geosite.dat
   ```

5. **检查上游凭据环境变量**

   如果 URL 使用了 `${UPSTREAM_USER}`，确保变量已导出：

   ```bash
   echo $UPSTREAM_USER
   ```

---

## 2. 客户端连接被拒绝

### 可能原因

1. `access_control.allowed_clients` 限制了来源 IP。
2. 全局 `max_connections` 已达上限。
3. 单用户 `max_conns_per_user` 已满。
4. 单 IP `rate_limit` 触发。

### 排查方法

查看日志关键字：

- `access denied` — 来源 IP 不在白名单。
- `connection limit exceeded` — 全局并发数满。
- `user connection limit exceeded` — 用户并发数满。
- `rate limited` — 触发速率限制。

临时放宽 ACL 进行验证：

```yaml
access_control:
  allowed_clients: []
  rate_limit:
    connections_per_second: 0
  max_conns_per_user: 0
```

---

## 3. 流量走了直连还是代理？

### 判断方法

日志中查找 `route` / `decision` 关键字，常见字段：

- `action=direct` — 直连。
- `action=proxy` — 走指定上游。
- `action=auto` — 自动选择，后续会缓存结果。
- `upstream=xxx` — 实际使用的上游名称。

若启用 `socks5h_redirect`，日志中可能看到 `socks5h_redirect` 尝试与回退信息。

### 验证方法

在目标上游侧抓包，查看是否收到来自 k5pw 的连接。

```bash
# 上游服务器上执行
sudo tcpdump -i any -n port 1080
```

---

## 4. 上游被标记为 dead 或频繁掉线

### 现象

日志中出现：

```text
upstream xxx dead
upstream xxx recovered
```

### 排查步骤

1. **确认上游可达**

   ```bash
   curl -x socks5://upstream:1080 https://www.example.com
   ```

2. **查看健康检查日志**

   `check_interval` 控制探测周期。健康检查会向上游发送最小协议握手（HTTP / SOCKS5），只有协议层正常响应才视为可用。

3. **调大超时或降低探测频率**

   ```yaml
   check_interval: 60
   timeouts:
     dial: 15s
     handshake: 15s
   ```

4. **检查上游是否间歇性拒绝连接**

   在 k5pw 主机上持续探测：

   ```bash
   while true; do nc -zv upstream 1080; sleep 5; done
   ```

---

## 5. 普通 HTTP 请求失败或重试异常

### 现象

普通 HTTP 请求返回 413 或连接异常。

### 原因

- 请求体超过 1 MiB 或请求使用 `chunked` 编码时，k5pw 会改为直接透传，透传时无法在上游失败时重试。
- 上游返回非 2xx 响应时，k5pw 会原样返回。

### 排查方法

1. 检查请求头 `Content-Length` 或 `Transfer-Encoding`。
2. 查看日志中 `request body too large` 或 `chunked request, forwarding directly` 提示。

---

## 6. SOCKS5 BIND / UDP ASSOCIATE 不工作

### 原因

- 当前上游为 HTTP/HTTPS 类型时，不支持 BIND 与 UDP ASSOCIATE，会自动回退本地直连。
- 防火墙或 NAT 阻止了反向/UDP 连接。

### 排查方法

1. 确认上游类型为 `socks5`/`socks5h`。
2. 在 k5pw 日志中查找 `bind` / `udp associate` 相关错误码。
3. 测试连通性：

   ```bash
   curl --socks5 127.0.0.1:4479 -p - ftp://example.com/
   ```

---

## 7. 性能问题：CPU 高 / 内存高

### 排查方法

1. 启用管理端口 pprof：

   ```yaml
   management: "127.0.0.1:8892"
   enable_pprof: true
   ```

2. 采集 CPU 与内存：

   ```bash
   go tool pprof http://127.0.0.1:8892/debug/pprof/profile?seconds=30
   go tool pprof http://127.0.0.1:8892/debug/pprof/heap
   ```

3. 检查 `max_connections` 是否设置过高，导致 goroutine 数量过大。
4. 检查 `rate_limit` 是否在每个 IP 上创建了过多限速器（长期未清理）。

---

## 8. 日志不滚动 / 不压缩

### 现象

日志文件无限增长，或 `*.gz` 文件未生成。

### 排查方法

1. 检查 `log.output` 是否为 `file`。
2. 检查 `log.rotation` 是 `size` 还是 `daily`。
3. 查看日志中是否有 `compress failed` 报错。
4. 检查文件系统权限：运行用户是否有写权限。

---

## 9. 配置热加载未生效

### 现象

修改并保存 `config.yml` 后，行为未改变。

### 排查步骤

1. 确认日志中是否有 `config reloaded`。
2. 管理端口 `/health` 返回的 `listen` 地址是否已更新。
3. 注意：`listen` 地址变更后，当前连接会短暂中断，新连接使用新端口。
4. 检查文件修改是否触发了 `fsnotify`（某些编辑器使用临时文件保存可能不触发）。

---

## 10. 抓包建议

### 本地回环抓包

```bash
sudo tcpdump -i lo -n -A port 4479
```

### 查看 SOCKS5 握手

```bash
sudo tcpdump -i lo -nn -X port 4479
```

### 查看 HTTPS  CONNECT 隧道

只能看到 `CONNECT` 明文，隧道建立后为加密流量。可通过 `openssl s_client -proxy` 单独测试上游。

---

## 11. 获取进程状态

```bash
curl http://127.0.0.1:8892/health
curl http://127.0.0.1:8892/metrics
```

`/metrics` 返回 Prometheus 格式，可用于排查：

- `k5pw_requests_total` — 请求总量。
- `k5pw_upstream_health` — 上游健康状态。
- `k5pw_transferred_bytes_total` — 流量。
- `k5pw_routing_cache_hits_total` — auto 缓存命中。

---

## 12. 仍无法定位？

建议收集以下信息提交 issue：

1. 运行版本：`k5pw -version`
2. 运行平台：`uname -a` / `go version`
3. 最小复现配置（去除上游凭据）。
4. 相关日志片段。
5. `/health` 与 `/metrics` 输出。

