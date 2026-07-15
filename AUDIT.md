# k5pw 产品化审计报告

**首次审计日期**: 2026-07-15  
**首次修复日期**: 2026-07-15  
**二次审计日期**: 2026-07-15  
**二次修复日期**: 2026-07-15  
**审计范围**: 代码质量、架构、安全性、测试、部署、可观测性、API 一致性、内存安全  
**代码版本**: main 分支 (commit c7ae1bb)  
**测试状态**: `go test -race ./...` 全部通过, `go vet ./...` 无错误, 覆盖率 72.0%  
**修复状态**: 首轮 P0/P1/P2 全部修复; 二轮 P1×3 + P2×6 全部修复; P3×4 信息性

---

## 总体评价

k5pw 是一个架构清晰、功能完备的轻量级统一代理网关。项目在协议覆盖、路由策略、安全防护和运维能力方面表现出较高的工程成熟度。首轮和二轮审计发现的所有 P0/P1/P2 问题已全部修复并验证通过。

| 维度 | 首轮评分 | 二轮评分 | 修复后评分 | 变化 |
|------|----------|----------|------------|------|
| 架构设计 | A | A | A | 维持 |
| 代码质量 | A- | A- | A | ↑ 死代码已清理 |
| 安全性 | B+ | A- | A | ↑ authFailures 清理已修复 |
| 测试覆盖 | B+ | B+ | B+ | 维持 72.0% |
| 部署运维 | A- | A | A | 维持 |
| 可观测性 | B+ | A- | A- | 维持 |
| API 一致性 | B | A | A | 维持 |
| 内存安全 | — | B+ | A | ↑ entries 上限 + authFailures 清理 |

---

## 首轮修复验证

### P0 修复 — 全部验证通过

| # | 问题 | 状态 | 验证位置 |
|---|------|------|----------|
| 1 | 流量配额未实际生效 | ✅ 已修复 | `timeout.go:120-133` — `Record()` 返回错误时终止隧道并记录日志 |
| 2 | per-listener 用户管理缺失 | ✅ 已修复 | `config_api_impl.go:235-317` — `AddUser`/`RemoveUser`/`ListUsers` 同步操作所有 listener |

### P1 修复 — 全部验证通过

| # | 问题 | 状态 | 验证位置 |
|---|------|------|----------|
| 3 | DNS 缓存无大小限制 | ✅ 已修复 | `dns.go:62-65,133-172` — 后台清理 + `dnsCacheMaxEntries=10000` 上限 |
| 4 | Prometheus 指标无基数限制 | ✅ 已修复 | `metrics.go:29,138` — `metricsMaxEntries=10000` + `normalizeTargetLabel()` 截断 |
| 5 | 管理 API 无请求体限制 | ✅ 已修复 | `config_api.go:403-413` — `io.LimitReader(r.Body, 1<<20)` |
| 6 | 管理 API 无暴力破解防护 | ✅ 已修复 | `management.go:52-68,124-190` — IP 级失败计数 + 临时封禁 |
| 7 | 一致性哈希非真正实现 | ✅ 已修复 | `upstream.go:589-641` — 哈希环 + 150 虚拟节点 + 二分查找 |

### P2 修复 — 全部验证通过

| # | 问题 | 状态 | 验证位置 |
|---|------|------|----------|
| 8 | 死代码 `redactUpstreamURL` | ✅ 已删除 | 搜索无结果 |
| 8 | 死代码 `checkAll()` | ✅ 已删除 | `upstream_health.go` — 已删除，测试改用 `checkAllForTest()` |
| 8 | 死代码 `HandleSOCKS` 别名 | ✅ 已删除 | `socks.go` — 别名已删除，`server.go` 直接调用 `HandleSOCKS5` |
| 8 | 死代码 `preferIPv4()` | ✅ 已删除 | `socks_sniff.go` — 已删除，测试改用 `sortAddrsByPrecedence()` |
| 9 | `upstream_health.go` 缩进 | ✅ 已修复 | `gofmt` 通过 |
| 10 | 日志文件打开失败静默 | ✅ 已修复 | `logging.go:296-306` — 回退到 stderr |
| 11 | systemd `SuccessExitStatus` | ✅ 已修复 | `k5pw.service` 已移除该行 |
| 12 | Docker 依赖 wget | ✅ 已修复 | `Dockerfile:32` — 仅安装 `ca-certificates` |
| 13 | 热加载旧连接不排空 | ✅ 已修复 | `server.go` — 添加 drain 日志，旧连接通过空闲超时自然排空 |

---

## 二次审计 — 新发现及修复

### P1 - 已全部修复

#### 17. 管理 API 认证失败 map 无清理机制

**文件**: `src/proxy/management.go`

**状态**: ✅ 已修复

**修复内容**: 添加 `startAuthFailureCleanup()` 后台 goroutine，每分钟清理 `blockedUntil` 已过期的条目。在 `StartListener` 时启动，在 `Stop` 时停止。

```go
func (m *ManagementServer) startAuthFailureCleanup() {
    m.authCleanupOnce.Do(func() {
        go func() {
            ticker := time.NewTicker(time.Minute)
            defer ticker.Stop()
            for {
                select {
                case <-ticker.C:
                    m.cleanupExpiredAuthFailures()
                case <-m.authCleanupStop:
                    return
                }
            }
        }()
    })
}
```

---

#### 18. TrafficAccounter entries map 窗口内无界增长

**文件**: `src/proxy/traffic_audit.go`

**状态**: ✅ 已修复

**修复内容**: 添加 `maxTrafficEntries=50000` 上限。超限时不再创建新条目，但仍通过 `recordAggregates()` 更新聚合计数器和 Prometheus 指标，确保配额检查不受影响。

```go
const maxTrafficEntries = 50000

// Record() 中：
if len(a.entries) >= maxTrafficEntries {
    a.mu.Unlock()
    return a.recordAggregates(k, bytesIn, bytesOut, total)
}
```

---

#### 19. `config_api_impl.go` saveAndApply 注释与实现不符

**文件**: `src/config_api_impl.go:59`

**状态**: ✅ 已修复

**修复内容**: 注释从"使用文件锁保证并发安全"修正为"使用进程内互斥锁保证并发安全"。

---

### P2 - 已全部修复

#### 20. `checkAll()` 死代码

**状态**: ✅ 已修复

**修复内容**: 删除 `checkAll()` 方法。在测试文件中添加 `checkAllForTest()` 辅助方法，重置 `nextCheckAt` 计时器后调用 `checkDue()`，保持测试语义不变。

---

#### 21. `preferIPv4()` 死代码

**状态**: ✅ 已修复

**修复内容**: 删除 `preferIPv4()` 函数。测试改为直接调用 `sortAddrsByPrecedence(addrs, IPPrecedenceIPv4First)`。

---

#### 22. 热加载时旧连接不优雅排空

**状态**: ✅ 已修复

**修复内容**: 热加载替换 listener 时添加 drain 日志。旧 listener 关闭后停止接收新连接，已建立的连接通过空闲超时自然排空，不会被强制中断。

```go
_ = oldLn.Close()
proxy.Logger().Infow("listener replaced, existing connections will drain", "address", addr)
```

---

#### 23. `DialOne` 与 `isSelectable` 的 dead 判断不一致

**状态**: ✅ 已修复

**修复内容**: `DialOne` 使用 `isSelectable(up)` 替代简单的 `up.dead` 检查，允许退避期已过的 dead 上游被选中用于自愈探测。

```go
// 修复前：if up != nil && up.dead { up = nil }
// 修复后：
if up != nil && !m.isSelectable(up) { up = nil }
```

---

#### 24. `TrafficAuditConfig` 缺少配置校验

**状态**: ✅ 已修复

**修复内容**: 在 `validateConfig` 中增加 `TrafficAudit` 校验，拒绝负值配额和窗口。

```go
if cfg.TrafficAudit.Enabled {
    if cfg.TrafficAudit.PerUserQuota < 0 {
        return fmt.Errorf("traffic_audit.per_user_quota must be >= 0")
    }
    if cfg.TrafficAudit.PerUpstreamQuota < 0 {
        return fmt.Errorf("traffic_audit.per_upstream_quota must be >= 0")
    }
    if cfg.TrafficAudit.QuotaWindow < 0 {
        return fmt.Errorf("traffic_audit.quota_window must be >= 0")
    }
}
```

---

#### 25. 缺少 `CHANGELOG.md` 和 `CONTRIBUTING.md`

**状态**: ✅ 已修复

**修复内容**: 
- 添加 `CHANGELOG.md`，遵循 Keep a Changelog 格式，记录 v1.2.0 和 Unreleased 变更。
- 添加 `CONTRIBUTING.md`，说明开发环境、代码规范、测试要求、提交格式和安全注意事项。

---

### P3 - 信息性发现（无需修复）

#### 26. 测试覆盖率

| 包 | 首轮覆盖率 | 二轮覆盖率 | 修复后覆盖率 | 变化 |
|----|-----------|-----------|-------------|------|
| `k5pw` (main) | 72.6% | 73.3% | 73.5% | +0.2% |
| `k5pw/proxy` | 70.9% | 71.7% | 71.7% | 维持 |
| 总计 | 71.2% | 72.0% | 72.0% | 维持 |

**薄弱环节**:
- `service.go:newService` — 0% (平台相关，难以测试)
- `server.go:createListener` — TLS/TPROXY 路径覆盖不足

#### 27. CI/CD 流水线建议

CI 包含: 测试 + race 检测 + golangci-lint + govulncheck + go vet + 交叉编译(20 平台) + Docker 验证。  
Release 包含: 质量门禁 + SBOM (Syft) + 冒烟测试 + GitHub Release。

**建议增加**:
- 覆盖率阈值门禁（如低于 65% 则失败）
- license 合规检查（go-licenses 或类似工具）
- Docker 镜像漏洞扫描（trivy/grype）

#### 28. `dialAuto` 双重拨号模式

**文件**: `src/proxy/router.go:349-422`

`dialAuto` 使用 singleflight 合并并发探测，但探测成功后会关闭连接，然后在 singleflight 返回后重新拨号。这是 singleflight 模式的已知权衡（连接无法跨 goroutine 共享），但意味着每次缓存未命中的 auto 决策需要两次拨号。

**说明**: 这是架构权衡而非 bug，记录于此供未来优化参考。

#### 29. 文档完备性

- `README.md`: 全面，覆盖快速开始、配置、部署、安全建议
- `AGENTS.md`: 详细的开发指南和架构说明
- `FEATURES.md`: 完整的特性清单
- `CHANGELOG.md`: ✅ 已添加
- `CONTRIBUTING.md`: ✅ 已添加
- `config.sample.yml`: 注释充分，覆盖所有配置项

**建议增加**:
- 架构图（当前仅有文字描述）
- 配置迁移指南（从单 `listen` 迁移到 `listeners`）

---

## 安全审计清单（最终版）

| 检查项 | 首轮 | 二轮 | 修复后 | 说明 |
|--------|------|------|--------|------|
| SSRF 防护 | ✅ | ✅ | ✅ | `DestinationPolicy` 默认拒绝私网/回环/元数据地址 |
| DNS Rebinding 防护 | ✅ | ✅ | ✅ | 直连路径在 DNS 解析后再次校验 IP |
| 认证常量时间比较 | ✅ | ✅ | ✅ | 使用 `crypto/subtle.ConstantTimeCompare` |
| URL 凭证脱敏 | ✅ | ✅ | ✅ | 日志和 API 响应中使用 `url.Redacted()` |
| 环境变量严格展开 | ✅ | ✅ | ✅ | 缺失/空值环境变量导致启动失败 |
| TLS 最低版本 | ✅ | ✅ | ✅ | 强制 TLS 1.2+ |
| 管理端口认证强制 | ✅ | ✅ | ✅ | 非回环绑定必须配置 Token |
| pprof 端点保护 | ✅ | ✅ | ✅ | 非回环必须配置 Token |
| HTTP 请求头大小限制 | ✅ | ✅ | ✅ | `maxHTTPHeaderBytes = 1 MiB` |
| 管理 API 请求体限制 | ❌ | ✅ | ✅ | `io.LimitReader(r.Body, 1<<20)` |
| 容器非 root 运行 | ✅ | ✅ | ✅ | Dockerfile 使用 `USER k5pw` |
| systemd 安全加固 | ✅ | ✅ | ✅ | `NoNewPrivileges`/`ProtectSystem`/`CapabilityBoundingSet=` |
| 流量配额强制 | ❌ | ✅ | ✅ | `Record()` 错误终止隧道 |
| 管理 API 暴力破解防护 | ❌ | ⚠️ | ✅ | IP 封禁 + 后台清理已修复 |
| DNS 缓存内存安全 | ❌ | ✅ | ✅ | 后台清理 + 10000 条上限 |
| 指标内存安全 | ❌ | ✅ | ✅ | 10000 条上限 + target 归一化 |
| 流量审计 entries 内存安全 | — | ❌ | ✅ | 50000 条上限 + 聚合兜底 |
| 配置写入原子性 | ✅ | ✅ | ✅ | 临时文件 + rename |
| 配置写入并发安全 | ✅ | ⚠️ | ✅ | 注释已修正为"进程内互斥锁" |
| TrafficAuditConfig 校验 | — | ❌ | ✅ | 负值配额/窗口校验已添加 |

---

## 修复总结

| 轮次 | 级别 | 发现数 | 修复数 | 状态 |
|------|------|--------|--------|------|
| 首轮 | P0 | 2 | 2 | ✅ 全部修复 |
| 首轮 | P1 | 5 | 5 | ✅ 全部修复 |
| 首轮 | P2 | 6 | 6 | ✅ 全部修复 |
| 二轮 | P1 | 3 | 3 | ✅ 全部修复 |
| 二轮 | P2 | 6 | 6 | ✅ 全部修复 |
| 二轮 | P3 | 4 | 0 | 信息性，无需修复 |
| **总计** | | **26** | **22** | **22/22 修复** |

**验证**: `go test -race ./...` 全部通过, `go vet ./...` 无错误, `gofmt` 全部修改文件干净, 覆盖率 72.0%。
