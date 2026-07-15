# k5pw 产品化审计报告

**审计日期**: 2026-07-15  
**修复日期**: 2026-07-15  
**审计范围**: 代码质量、架构、安全性、测试、部署、可观测性、API 一致性  
**代码版本**: main 分支 (领先 origin/main 2 个提交)  
**修复状态**: P0 全部修复, P1 全部修复, P2 全部修复

---

## 总体评价

k5pw 是一个架构清晰、功能完备的轻量级统一代理网关。项目在协议覆盖、路由策略、安全防护和运维能力方面表现出较高的工程成熟度。以下审计从产品化角度识别出若干需要关注的问题，按严重程度分级。

| 维度 | 评分 | 说明 |
|------|------|------|
| 架构设计 | A | 模块分层清晰，职责边界明确 |
| 代码质量 | A- | 风格一致，注释充分，少量死代码 |
| 安全性 | B+ | SSRF 防护完善，但配额未实际生效 |
| 测试覆盖 | B+ | 71.2% 覆盖率，CI 含 race 检测 |
| 部署运维 | A- | Docker/systemd/CI/CD 齐全 |
| 可观测性 | B+ | 指标完善，但存在内存泄漏风险 |
| API 一致性 | B | 多 listener 用户管理缺失 |

---

## P0 - 必须修复

### 1. 流量配额未实际生效

**文件**: `src/proxy/timeout.go:120-125`

`TrafficAccounter.Record()` 在流量超出配额时返回错误，但调用方 `deadlineReader.Read()` 使用 `_ =` 忽略了返回值。这意味着配额限制形同虚设——流量超出配额后仍继续转发。

```go
// timeout.go:120-125 — 当前代码
if n > 0 && r.accounter != nil {
    if r.auditDirection == "in" {
        _ = r.accounter.Record(r.trafficKey, int64(n), 0)  // 错误被忽略
    } else {
        _ = r.accounter.Record(r.trafficKey, 0, int64(n))  // 错误被忽略
    }
}
```

**影响**: `traffic_audit.per_user_quota` 和 `per_upstream_quota` 配置项声称的配额限制不会中断流量转发，属于功能与文档不符。

**建议**: 在 `Record()` 返回配额超限错误时，应终止隧道转发（关闭连接或停止 `io.Copy`）。需要在 `deadlineReader` 中增加一个 `quotaExceeded` 标志，由 `Tunnel` 函数检查并中断转发。

---

### 2. 运行时配置 API 不支持 per-listener 用户管理

**文件**: `src/config_api_impl.go:235-283`

当配置使用 `listeners`（多监听端口，这是推荐模式）时，每个 listener 拥有独立的 `users` map。但 `configEditor` 的 `AddUser`/`RemoveUser`/`ListUsers` 仅操作全局 `cfg.Users`，不触及 `cfg.Listeners[].Users`。

```go
// config_api_impl.go:259-265 — 仅操作全局 users
if cfg.Users == nil {
    cfg.Users = make(map[string]string)
}
if _, exists := cfg.Users[username]; exists {
    return fmt.Errorf("user %q already exists", username)
}
cfg.Users[username] = password
```

**影响**: 在多 listener 模式下，通过 API 添加的用户不会生效到任何 listener；删除用户也不会影响 listener 级别的认证。API 返回的用户列表也不包含 listener 级别的用户。

**建议**: 
- 当 `len(cfg.Listeners) > 0` 时，API 应操作 listener 级别的 users（可选择操作所有 listener 或指定 listener）。
- `ListUsers` 应聚合全局和所有 listener 的用户名。
- 或在 API 中增加 `listener` 参数以指定目标 listener。

---

## P1 - 建议修复

### 3. DNS 缓存无大小限制，存在内存泄漏

**文件**: `src/proxy/dns.go:329-347`

DNS 缓存 map 仅在访问时检查过期，没有后台清理机制。长期运行的代理处理大量唯一域名时，过期条目会无限积累。

```go
// dns.go:329-337 — 仅在 cacheGet 时检查过期，无主动清理
func (r *DNSResolver) cacheGet(host string) ([]net.IPAddr, bool) {
    r.cacheMu.RLock()
    defer r.cacheMu.RUnlock()
    entry, ok := r.cache[host]
    if !ok || time.Now().After(entry.expiresAt) {
        return nil, false  // 过期但不删除
    }
    return entry.addrs, true
}
```

**建议**: 增加后台定期清理 goroutine（类似 `AccessController.StartCleanup`），或使用带容量限制的 LRU 缓存。

---

### 4. Prometheus 指标无标签基数限制

**文件**: `src/proxy/metrics.go:129-134`

`k5pw_traffic_bytes_total` 指标包含 `target` 标签（目标域名），高基数标签会导致指标 map 无限增长。每个唯一的目标域名都会创建新的指标条目，且永不清理。

```go
// traffic_audit.go:193-194 — target 标签为高基数
MetricAdd("k5pw_traffic_bytes_total", float64(bytesIn), 
    "direction=client_to_target", "user="+k.User, 
    "upstream="+k.Upstream, "target="+k.Target)
```

**影响**: 长期运行后 `/metrics` 端点响应越来越大，内存持续增长。

**建议**: 
- 对 `target` 标签做归一化或截断（如只保留域名前 N 字符，或按域名后缀聚合）。
- 或从 Prometheus 指标中移除 `target` 标签，仅在 `/api/v1/traffic` JSON 响应中保留明细。
- 增加指标条目上限和 LRU 淘汰。

---

### 5. 管理 API 无请求体大小限制

**文件**: `src/proxy/config_api.go:403-410`

`decodeJSON` 直接从 `r.Body` 读取，无大小限制。攻击者可通过管理端口发送超大 JSON 请求体耗尽内存。

```go
func decodeJSON(r *http.Request, v any) error {
    dec := json.NewDecoder(r.Body)
    dec.DisallowUnknownFields()
    if err := dec.Decode(v); err != nil {
        return fmt.Errorf("invalid JSON: %w", err)
    }
    return nil
}
```

**建议**: 使用 `io.LimitReader(r.Body, maxBodySize)` 限制请求体大小（如 1 MiB）。

---

### 6. 管理 API 无速率限制/暴力破解防护

**文件**: `src/proxy/management.go:106-123`

管理端口的 Bearer Token 认证使用 `subtle.ConstantTimeCompare` 防止时序攻击，但没有失败次数限制或速率限制。攻击者可高速暴力破解管理 Token。

**建议**: 
- 对管理端口增加 IP 级别的请求速率限制。
- 或在连续认证失败后增加指数退避。
- 管理端口绑定非回环地址时，建议增加更严格的速率限制。

---

### 7. `pickConsistentHash` 非真正一致性哈希

**文件**: `src/proxy/upstream.go:589-597`

当前实现是 `fnv(key) % len(us)`，添加或移除上游时几乎所有 key 都会重新映射。真正的一致性哈希应使用哈希环（hash ring），保证只有部分 key 需要迁移。

```go
func pickConsistentHash(us []*Upstream, key string) *Upstream {
    if key == "" {
        return pickWeighted(us)
    }
    h := fnv.New32a()
    h.Write([]byte(key))
    idx := h.Sum32() % uint32(len(us))  // 简单取模，非一致性哈希
    return us[idx]
}
```

**影响**: `consistent_hash` 策略声称的"会话保持"在上游扩缩容时大面积失效。

**建议**: 实现标准的哈希环（虚拟节点），或修改文档明确说明当前实现的局限性。

---

## P2 - 改进建议

### 8. 死代码清理

| 位置 | 说明 |
|------|------|
| `src/proxy/config_api.go:412-419` | `redactUpstreamURL` 函数定义但从未调用（实际使用 `redactURLString`） |
| `src/proxy/upstream_health.go:158-198` | `checkAll()` 方法仅在测试中调用，生产代码未使用 |
| `src/proxy/socks.go:90-93` | `HandleSOCKS` 是 `HandleSOCKS5` 的冗余别名 |

**建议**: 删除死代码或添加 `// Deprecated` 注释。

---

### 9. `upstream_health.go` 代码缩进不一致

**文件**: `src/proxy/upstream_health.go:172-186`

`checkAll()` 方法内部缩进混乱，`else if` 块与外层 `if` 不对齐，疑似编辑遗留。

```go
// upstream_health.go:166-186 — 缩进不一致
        if u.dead && alive {
            ...
            u.nextProbeAt = time.Time{}
    } else if !u.dead {  // 缩进异常
        // alive 上游重置失败计数
        if alive {
```

**建议**: 运行 `gofmt` 修复格式。

---

### 10. 日志文件打开失败静默忽略

**文件**: `src/proxy/logging.go:295-302`

`dailyRotateWriter.reopen()` 在文件打开失败时仅设置 `w.file = nil`，不返回错误。后续写入会失败但错误仅记录到日志（此时日志系统本身已故障），可能导致日志丢失。

```go
func (w *dailyRotateWriter) reopen() {
    f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
    if err != nil {
        return  // 静默失败
    }
    w.file = f
    w.date = time.Now()
}
```

**建议**: 打开失败时回退到 stderr 输出，或记录到系统日志。

---

### 11. systemd `SuccessExitStatus` 配置不当

**文件**: `deploy/systemd/k5pw.service:11`

```ini
SuccessExitStatus=1 2 8 SIGKILL
```

退出码 1 和 2 通常是错误退出，将它们标记为"成功"会掩盖真实的启动失败。`SIGKILL` (9) 作为成功退出也不合理——被 SIGKILL 通常意味着进程被强制终止。

**建议**: 移除此行，或仅保留 `SIGTERM`（如果确实需要将信号终止视为正常停止）。

---

### 12. Docker 健康检查依赖 wget

**文件**: `deploy/docker/Dockerfile:32`

```dockerfile
RUN apk add --no-cache ca-certificates wget && \
```

仅为健康检查安装 `wget`，增加了镜像体积。

**建议**: 使用 Go 编写的轻量健康检查二进制，或使用 `curl` 的 slim 版本，或在 entrypoint 中内置健康检查逻辑。

---

### 13. 热加载时旧连接不优雅排空

**文件**: `src/server.go:270-287`

热加载替换 listener 时，旧 listener 被关闭但已建立的连接不会被等待。虽然 `Shutdown` 有连接等待逻辑，但热加载路径（`applyConfig`）不调用 `Shutdown`。

**影响**: 热加载时活跃连接会被立即中断。

**建议**: 在热加载路径中，对被替换的 listener 的活跃连接增加短暂的 drain 窗口。

---

## P3 - 信息性发现

### 14. 测试覆盖率分布

| 包 | 覆盖率 | 说明 |
|----|--------|------|
| `k5pw` (main) | 72.6% | server.go 核心路径覆盖良好 |
| `k5pw/proxy` | 70.9% | 协议层覆盖良好 |
| 总计 | 71.2% | 高于行业平均 |

**薄弱环节**:
- `service.go:newService` — 0% (平台相关，难以测试)
- `server.go:createListener` — 66.7% (TLS/TPROXY 路径)
- `server.go:handle` — 62.9% (协议分发)

### 15. CI/CD 流水线完善

CI 包含: 测试 + race 检测 + golangci-lint + govulncheck + go vet + 交叉编译 + Docker 验证。  
Release 包含: 质量门禁 + SBOM (Syft) + 冒烟测试 + GitHub Release。

**建议增加**:
- 覆盖率阈值门禁（如低于 65% 则失败）
- license 合规检查
- Docker 镜像漏洞扫描（trivy/grype）

### 16. 文档完备性

- `README.md`: 全面，覆盖快速开始、配置、部署、安全建议
- `AGENTS.md`: 详细的开发指南和架构说明
- `FEATURES.md`: 完整的特性清单
- `config.sample.yml`: 注释充分，覆盖所有配置项

**建议增加**:
- `CHANGELOG.md`（当前依赖 GitHub Release Notes）
- `CONTRIBUTING.md`
- 架构图（当前仅有文字描述）

---

## 安全审计清单

| 检查项 | 状态 | 说明 |
|--------|------|------|
| SSRF 防护 | ✅ | `DestinationPolicy` 默认拒绝私网/回环/元数据地址 |
| DNS Rebinding 防护 | ✅ | 直连路径在 DNS 解析后再次校验 IP |
| 认证常量时间比较 | ✅ | 使用 `crypto/subtle.ConstantTimeCompare` |
| URL 凭证脱敏 | ✅ | 日志和 API 响应中使用 `url.Redacted()` |
| 环境变量严格展开 | ✅ | 缺失/空值环境变量导致启动失败 |
| TLS 最低版本 | ✅ | 强制 TLS 1.2+ |
| 管理端口认证强制 | ✅ | 非回环绑定必须配置 Token |
| pprof 端点保护 | ✅ | 非回环必须配置 Token |
| HTTP 请求头大小限制 | ✅ | `maxHTTPHeaderBytes = 1 MiB` |
| 请求体大小限制 | ✅ | `maxBufferedRequestBody = 1 MiB` |
| 容器非 root 运行 | ✅ | Dockerfile 使用 `USER k5pw` |
| systemd 安全加固 | ✅ | `NoNewPrivileges`/`ProtectSystem`/`cap_drop: ALL` |
| 流量配额强制 | ❌ | 配额错误被忽略（见 P0-1） |
| 管理 API 速率限制 | ❌ | 无暴力破解防护（见 P1-6） |
| 管理 API 请求体限制 | ❌ | 无大小限制（见 P1-5） |

---

## 修复优先级建议

1. **立即修复** (P0): 流量配额生效、per-listener 用户管理
2. **本迭代修复** (P1): DNS 缓存清理、指标基数限制、管理 API 加固、一致性哈希
3. **下迭代修复** (P2): 死代码清理、格式修复、日志容错、systemd 配置
4. **持续改进** (P3): 测试覆盖率、CI 增强、文档补充
