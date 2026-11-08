## 变更说明

<!-- 说明为什么改，而不只是改了什么 -->

## 类型

- [ ] `fix` 缺陷修复
- [ ] `feat` 新功能
- [ ] `refactor` 重构
- [ ] `docs` / `test` / `chore`

## 检查清单

- [ ] `cd src && go test ./...` 与 `go test -race ./...` 通过
- [ ] `cd web && npm run type-check && npm run check && npm test` 通过
- [ ] 涉及代理协议时覆盖 HTTP、CONNECT、SOCKS5 三类路径
- [ ] 涉及配置文件时同步更新 `src/config.sample.yml` 与相关文档
- [ ] 涉及平台清单时同步 `scripts/platforms.sh`、`.github/workflows/ci.yml`、`scripts/build.ps1`
- [ ] 未提交任何令牌、口令、私有地址或运行数据
- [ ] 用户可见行为变化时更新 `CHANGELOG.md`
