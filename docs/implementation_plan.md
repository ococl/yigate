# k5pw 产品化实施计划

## 目标

消除产品化检查中发现的问题与风险，使 k5pw 在协议覆盖、健壮性、安全性和可维护性方面达到可发布状态。

## 问题清单与优先级

| 优先级 | 问题 | 影响 | 处理方案 |
|---|---|---|---|
| P0 | 上游凭据在 URL 中明文 | 安全风险，泄露上游认证信息 | 支持通过环境变量注入上游用户名/密码 |
| P0 | 健康检查仅 TCP 拨号 | 无法识别协议层故障 | 改为协议级探测（HTTP/SOCKS5 握手） |
| P1 | HTTP 普通请求不支持 chunked 编码 | 部分现代客户端/请求会失败 | 对 chunked 请求改为直接透传，禁用重试但保证可用 |
| P1 | SOCKS5 不支持 BIND/UDP ASSOCIATE | 功能缺口，不符合完整 SOCKS5 标准 | 已实现 CONNECT/BIND/UDP ASSOCIATE 三种命令，BIND/UDP ASSOCIATE 支持通过 SOCKS5 上游级联，失败时自动回退本地直连 |
| P1 | 监听地址变更需重启 | 配置热加载不完整 | 支持 `listen` 变更时平滑重启监听（先启动新 listener，再关闭旧 listener） |
| P2 | 请求体缓冲 1 MiB 限制 | 大文件上传受限 | 超大请求体改为直接透传，不缓冲；或把限制改为可配置项 |
| P2 | socks5h 嗅探失败回退 | 可能受 DNS 污染影响 | 保持现有回退，但增加更明确的日志和统计 |

## 实施顺序

1. **协议级健康检查**：修改 `proxy/upstream.go` 的 `check` 方法，根据上游类型发送最小协议握手。
2. **上游凭据环境变量**：修改 `proxy/upstream.go` / `config.go`，在解析上游 URL 时支持 `${ENV_NAME}` 替换。
3. **HTTP chunked 支持**：修改 `proxy/http.go`，对 `TransferEncoding: chunked` 请求直接透传，避免缓冲和重试。
4. **SOCKS5 标准错误码**：修改 `proxy/socks.go`，对非 CONNECT 命令返回 `0x07`，对不支持的地址类型返回 `0x08`。
5. **监听地址热加载**：修改 `server.go`，在 `LoadAndApply` 检测到 `listen` 变化时切换 listener。
6. **大请求体透传**：在 chunked 透传完成后，同步处理超过 1 MiB 的非 chunked 请求直接透传。
7. **回归测试**：执行 `go test ./...` 和 `test_proxy_4479.py`，确认无回归。

## 验收标准

- `go test ./...` 全部通过。
- `test_proxy_4479.py` 全部通过或仅保留站点侧告警。
- 文档（README、AGENTS）更新对应变更。
- 不再出现明文上游密码示例（`src/config.yml` 除外，因其被 `.gitignore` 忽略）。

## 备注

- 本计划按“小步快跑”原则，优先解决安全和可用性问题，完整 SOCKS5 协议覆盖（CONNECT/BIND/UDP ASSOCIATE）已在本轮迭代完成。
- 所有变更完成后应再次提交一次代码。
