# Changelog

本项目变更日志遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Fixed

- **流量审计 entries 内存安全**: `TrafficAccounter.entries` 增加上限 `maxTrafficEntries=50000`，超限时不再创建新条目，防止高流量代理在配额窗口内内存无限增长。
- **管理 API 认证失败 map 清理**: `authFailures` map 增加后台定期清理（每分钟），删除封禁已过期的 IP 条目，防止公网暴露时无限增长。
- **DialOne 自愈一致性**: `DialOne` 使用 `isSelectable` 逻辑替代简单的 `dead` 检查，允许退避期已过的 dead 上游被选中用于自愈探测。
- **配置注释修正**: `saveAndApply` 注释从"文件锁"修正为"进程内互斥锁"，与实际实现一致。
- **TrafficAuditConfig 校验**: `validateConfig` 增加 `per_user_quota`、`per_upstream_quota`、`quota_window` 负值校验。

### Changed

- **热加载 listener drain**: 热加载替换 listener 时添加 drain 日志，明确旧连接通过空闲超时自然排空。
- **HandleSOCKS 别名移除**: 删除 `HandleSOCKS` 别名，直接使用 `HandleSOCKS5`。

### Removed

- **死代码清理**: 删除 `checkAll()`（仅测试调用，已由 `checkDue()` 替代）、`preferIPv4()`（已由 `sortAddrsByPrecedence()` 替代）。

## [1.2.0] - 2026-07-15

### Added

- 流量配额强制生效：`Record()` 返回配额超限错误时终止隧道转发。
- per-listener 用户管理：`AddUser`/`RemoveUser`/`ListUsers` 同步操作所有 listener。
- DNS 缓存后台清理 + 10000 条上限。
- Prometheus 指标基数限制 + target 标签归一化。
- 管理 API 请求体限制（1 MiB）。
- 管理 API IP 级暴力破解防护。
- 一致性哈希标准哈希环实现（150 虚拟节点）。
- systemd 安全加固（NoNewPrivileges/ProtectSystem/CapabilityBoundingSet）。
- Docker 容器非 root 运行。
- 日志文件打开失败回退到 stderr。

### Changed

- 环境变量严格展开：缺失或空值导致启动失败。
- TLS 最低版本强制 TLS 1.2+。
- 管理端口非回环绑定必须配置 `management_auth_token`。
