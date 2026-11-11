#!/usr/bin/env bash
set -euo pipefail

# version.sh — 根据CI上下文确定构建版本号。
#
# 版本方案:
#   Tag push (vX.Y.Z)             → X.Y.Z                    (正式版)
#   Tag push (vX.Y.Z-<prerelease>) → X.Y.Z-<prerelease>      (预发布版，如 0.8.0-beta1)
#   main 分支 push                 → 0.0.0-beta.<short>       (按提交自动构建的 Beta)
#   其他                           → 0.0.0-dev.<short>        (开发版)
#
# 依赖 CNB 内置环境变量:
#   CNB_IS_TAG       — 是否为 Tag 构建
#   CNB_BRANCH       — 分支名或 Tag 名
#   CNB_COMMIT_SHORT — 短 commit SHA
#
# 本地运行时 (非CI环境) 使用 git 命令回退取值。

IS_TAG="${CNB_IS_TAG:-false}"
BRANCH="${CNB_BRANCH:-}"
COMMIT_SHORT="${CNB_COMMIT_SHORT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"

if [[ "$IS_TAG" == "true" ]]; then
    # Tag push: 去掉 v 前缀，校验 semver 格式（允许预发布后缀，如 v0.8.0-beta1）
    VERSION="${BRANCH#v}"
    if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$ ]]; then
        echo "error: tag '$BRANCH' 不是有效的版本号 (期望 vX.Y.Z 或 vX.Y.Z-<prerelease>)" >&2
        exit 1
    fi
    echo "$VERSION"
elif [[ "$BRANCH" == "main" ]]; then
    echo "0.0.0-beta.${COMMIT_SHORT}"
else
    echo "0.0.0-dev.${COMMIT_SHORT}"
fi
