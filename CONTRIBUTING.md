# 贡献指南

感谢你对 k5pw 项目的关注！本文档说明了参与贡献的流程和规范。

## 开发环境

- Go 1.21+
- 依赖使用国内镜像：`GOPROXY=https://goproxy.cn,direct`

## 开发流程

1. **Fork 并克隆仓库**，创建功能分支：`git checkout -b feature/your-feature`。
2. **保持改动小而聚焦**，避免无关重构。
3. **编写测试**：新增功能或修复 bug 时应补充对应测试。
4. **本地验证**：确保以下命令全部通过：
   ```bash
   cd src
   GOPROXY=https://goproxy.cn,direct go test ./...
   GOPROXY=https://goproxy.cn,direct go vet ./...
   ```
5. **提交代码**，提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/) 格式。
6. **创建 Pull Request**，描述改动内容和动机。

## 代码规范

- 遵循 `gofmt` 和 `go vet` 标准。
- 协议入口改动需覆盖 HTTP、CONNECT、SOCKS5 三类路径。
- 路由逻辑改动应补充或更新 `router`、`route_cache`、`socks5h` 相关测试。
- 上游选择逻辑改动应关注优先级、权重、dead 标记、健康检查。
- 热加载逻辑改动应确认旧组件停止、新组件原子替换。
- 不要提交真实代理密码、管理 Token、上游账号或私有地址。

## 提交信息格式

```
<type>(<scope>): <subject>

<body>
```

**type**: `feat`（新功能）、`fix`（修复）、`refactor`（重构）、`docs`（文档）、`test`（测试）、`chore`（杂项）

**scope**: 可选，影响的模块名（如 `proxy`、`config`、`server`、`deploy`）

示例：
```
feat(proxy): add consistent hash upstream selection
fix(traffic-audit): enforce quota by terminating tunnel on limit exceeded
docs(readme): add TLS configuration guide
```

## 测试指南

- 单元测试放在 `*_test.go` 文件中，与被测代码同包。
- 使用标准 `testing` 包，避免引入额外测试框架。
- 协议测试可使用 `net.Pipe()` 或本地 mock 服务器。
- 热加载测试应验证旧组件正确停止、新组件原子替换。

## 安全注意事项

- 不要在代码或配置示例中使用真实凭证。
- pprof 和管理接口属于敏感入口，不应默认暴露到公网。
- 目标策略（`destination_policy`）是 SSRF 防护的最后防线，改动需谨慎。
- TLS 终止在 listener 层统一包装，协议层应保持无感知。

## 报告问题

- 使用 GitHub Issues 报告 bug 或提出功能建议。
- 安全漏洞请私下联系维护者，不要公开 Issue。
