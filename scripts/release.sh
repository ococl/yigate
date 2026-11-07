#!/usr/bin/env bash
set -euo pipefail

# release.sh — 从已有 Git tag 构建正式发布制品。
# 用法: ./scripts/release.sh <version>
# 前置: Git 工作区干净，tag v<version> 已存在且指向当前 commit。
# 不创建、修改或推送 Git tag。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="${REPO_ROOT}/src"
WEB_DIR="${REPO_ROOT}/web"
WEBUI_DIST="${SRC_DIR}/proxy/webui_dist"
SCRIPTS_DIR="${REPO_ROOT}/scripts"

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "用法: $0 <version>" >&2
  exit 1
fi

# 校验版本格式
if [[ ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: 版本号必须为 x.y.z 格式" >&2
  exit 1
fi

TAG="v${VERSION}"

# 前置校验：工作区干净
if [[ -n "$(git -C "${REPO_ROOT}" status --porcelain)" ]]; then
  echo "error: Git 工作区不干净，拒绝发布" >&2
  exit 1
fi

# 前置校验：tag 存在且指向当前 commit
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

# 版本元数据
COMMIT="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
BUILD_TIME="$(git -C "${REPO_ROOT}" log -1 --format=%cI "${TAG}")"

echo "Building yigate ${TAG} (commit ${COMMIT:0:12}, built ${BUILD_TIME})"

# 前端必须先构建并嵌入，禁止发布占位 WebUI。
(
  cd "${WEB_DIR}"
  npm ci
  npm run build
)
node "${WEB_DIR}/scripts/embed-dist.mjs" "${WEB_DIR}/dist" "${WEBUI_DIST}"
if ! find "${WEBUI_DIST}/assets" -maxdepth 1 -type f \( -name '*.js' -o -name '*.js.gz' \) -print -quit | grep -q .; then
  echo "error: WebUI 构建产物缺少 JS assets，拒绝发布" >&2
  exit 1
fi

if ! command -v syft >/dev/null 2>&1 && ! command -v docker >/dev/null 2>&1; then
  echo "error: 正式发布需要 syft 或 Docker 生成 SBOM" >&2
  exit 1
fi

# 编译、打包、SBOM 与校验和统一交给唯一构建入口。
YIGATE_VERSION="${VERSION}" \
YIGATE_COMMIT="${COMMIT}" \
YIGATE_BUILD_TIME="${BUILD_TIME}" \
YIGATE_REQUIRE_SBOM=1 \
  "${SCRIPTS_DIR}/ci-build.sh" "${VERSION}"

echo "Release artifacts written to ${REPO_ROOT}/release"
