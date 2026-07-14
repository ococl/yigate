#!/usr/bin/env bash
set -euo pipefail

# release.sh — 从已有 Git tag 构建正式发布制品。
# 用法: ./scripts/release.sh <version>
# 前置: Git 工作区干净，tag v<version> 已存在且指向当前 commit。
# 不创建、修改或推送 Git tag。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="${REPO_ROOT}/src"
RELEASE_DIR="${REPO_ROOT}/release"
SCRIPTS_DIR="${REPO_ROOT}/scripts"

source "${SCRIPTS_DIR}/platforms.sh"

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

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

echo "Building k5pw ${TAG} (commit ${COMMIT:0:12}, built ${BUILD_TIME})"

# 清空并重建 release staging
rm -rf "${RELEASE_DIR}"
mkdir -p "${RELEASE_DIR}"

# 依赖校验
(
  cd "${SRC_DIR}"
  GOPROXY=https://goproxy.cn,direct go mod verify
)

# 跨平台构建
for platform in $(all_platforms); do
  name="$(output_name "${VERSION}" "${platform}")"
  env_str="$(build_env "${platform}")"
  echo "Building ${name}..."
  (
    cd "${SRC_DIR}"
    env ${env_str} CGO_ENABLED=0 GOPROXY=https://goproxy.cn,direct \
      go build -trimpath -mod=readonly -ldflags "${LDFLAGS}" \
      -o "${RELEASE_DIR}/${name}" .
  )
done

# 生成 Linux 安装包
for platform in $(linux_platforms); do
  suffix="$(normalize_platform "${platform}")"
  "${SCRIPTS_DIR}/package-linux.sh" "${VERSION}" "${suffix}"
done

# 生成 SBOM
syft dir "${SRC_DIR}" -o spdx-json > "${RELEASE_DIR}/k5pw-${VERSION}.spdx.json"

# 生成校验和
(
  cd "${RELEASE_DIR}"
  sha256sum k5pw-* k5pw-*.spdx.json > SHA256SUMS
  sha256sum -c SHA256SUMS
)

echo "Release artifacts written to ${RELEASE_DIR}"
