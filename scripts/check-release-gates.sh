#!/usr/bin/env bash
set -euo pipefail

# check-release-gates.sh — 所有发布通道共用的门禁。
# 用法: check-release-gates.sh <version> [--tag]
#   --tag  额外要求 v<version> 标签存在且指向 HEAD（仅正式发布）。
# 不创建、修改或推送任何 Git 对象。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-}"
MODE="${2:-}"

if [[ -z "${VERSION}" ]]; then
  echo "用法: $0 <version> [--tag]" >&2
  exit 1
fi

if [[ ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-(beta|dev)\.[0-9a-f]+)?$ ]]; then
  echo "error: 版本号必须为 x.y.z 或 x.y.z-beta.<sha> / x.y.z-dev.<sha>" >&2
  exit 1
fi

if [[ -n "$(git -C "${REPO_ROOT}" status --porcelain)" ]]; then
  echo "error: Git 工作区不干净，拒绝发布" >&2
  git -C "${REPO_ROOT}" status --short >&2 || true
  exit 1
fi

# 正式版（纯 x.y.z）自动要求 tag；Beta/Dev 版不要求，避免两条流水线写成两份门禁。
if [[ "${MODE}" == "--tag" || "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  TAG="v${VERSION}"
  if ! git -C "${REPO_ROOT}" rev-parse "${TAG}" >/dev/null 2>&1; then
    echo "error: tag ${TAG} 不存在" >&2
    exit 1
  fi
  TAG_COMMIT="$(git -C "${REPO_ROOT}" rev-list -n1 "${TAG}")"
  HEAD_COMMIT="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
  if [[ "${TAG_COMMIT}" != "${HEAD_COMMIT}" ]]; then
    echo "error: tag ${TAG} 指向 ${TAG_COMMIT:0:8}，但 HEAD 为 ${HEAD_COMMIT:0:8}" >&2
    exit 1
  fi
elif [[ -n "${MODE}" ]]; then
  echo "error: 未知参数 ${MODE}（可选: --tag）" >&2
  exit 1
fi

echo "release gates passed: ${VERSION} ${MODE}"
