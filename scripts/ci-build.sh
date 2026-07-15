#!/usr/bin/env bash
set -euo pipefail

# ci-build.sh — CI 环境下的跨平台构建脚本。
#
# 功能: 依赖下载 → 跨平台编译 → 全平台打包 → SBOM(可选) → 校验和
# 用法: scripts/ci-build.sh [version]
#   version 可选，未提供时调用 scripts/version.sh 自动确定
#
# 产物输出到 release/ 目录，版本号写入 release/.version 供下游阶段使用。
# 产物仅包含压缩归档 (tar.gz/zip) + SHA256SUMS，不含裸二进制。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="${REPO_ROOT}/src"
RELEASE_DIR="${REPO_ROOT}/release"
SCRIPTS_DIR="${REPO_ROOT}/scripts"

source "${SCRIPTS_DIR}/platforms.sh"

# 确定版本号
VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    VERSION="$(bash "${SCRIPTS_DIR}/version.sh")"
fi

COMMIT="${CNB_COMMIT:-$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${CNB_BUILD_START_TIME:-$(git -C "${REPO_ROOT}" log -1 --format=%cI 2>/dev/null || echo unknown)}"

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

echo "=== Building YiGate ${VERSION} (commit ${COMMIT:0:12}) ==="

# 准备 release 目录
rm -rf "${RELEASE_DIR}"
mkdir -p "${RELEASE_DIR}"
echo "$VERSION" > "${RELEASE_DIR}/.version"

# 下载依赖
echo "--- Downloading dependencies ---"
(
    cd "${SRC_DIR}"
    GOPROXY=https://goproxy.cn,direct go mod download
)

# 跨平台编译
echo "--- Cross-compiling $(all_platforms | wc -l) platforms ---"
for platform in $(all_platforms); do
    name="$(output_name "${VERSION}" "${platform}")"
    env_str="$(build_env "${platform}")"
    echo "  Building ${name}..."
    (
        cd "${SRC_DIR}"
        env ${env_str} CGO_ENABLED=0 GOPROXY=https://goproxy.cn,direct \
            go build -trimpath -ldflags "${LDFLAGS}" \
            -o "${RELEASE_DIR}/${name}" .
    )
done

# 全平台打包
echo "--- Packaging $(all_platforms | wc -l) archives ---"
for platform in $(all_platforms); do
    "${SCRIPTS_DIR}/package.sh" "${VERSION}" "${platform}"
done

# 删除裸二进制，仅保留压缩归档
echo "--- Removing raw binaries ---"
for platform in $(all_platforms); do
    name="$(output_name "${VERSION}" "${platform}")"
    rm -f "${RELEASE_DIR}/${name}"
done

# 生成 SBOM (如果 syft 可用)
if command -v syft >/dev/null 2>&1; then
    echo "--- Generating SBOM ---"
    syft dir "${SRC_DIR}" -o spdx-json > "${RELEASE_DIR}/yigate-${VERSION}.spdx.json"
else
    echo "--- Skipping SBOM (syft not installed) ---"
fi

# 生成校验和
echo "--- Generating checksums ---"
(
    cd "${RELEASE_DIR}"
    find . -maxdepth 1 -type f ! -name '.version' ! -name 'SHA256SUMS' -printf '%P\n' | sort | xargs sha256sum > SHA256SUMS
    sha256sum -c SHA256SUMS
)

echo "=== Build complete ==="
echo "Version: ${VERSION}"
echo "Artifacts:"
ls -la "${RELEASE_DIR}/"
