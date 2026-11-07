#!/usr/bin/env bash
set -euo pipefail

# ci-build.sh — Unix 环境下唯一的正式制品构建入口。
#
# 功能: 依赖校验 → 跨平台编译 → 全平台打包 → SBOM → 校验和
# 用法: scripts/ci-build.sh [version]
#   version 可选，未提供时调用 scripts/version.sh 自动确定。
#
# 可覆盖环境变量:
#   YIGATE_VERSION       版本号（优先于位置参数）
#   YIGATE_COMMIT        构建提交
#   YIGATE_BUILD_TIME    构建时间
#   YIGATE_REQUIRE_SBOM  为 1 时 SBOM 缺失即失败（正式发布使用）
#
# 产物输出到 release/ 目录，版本号写入 release/.version 供下游阶段使用。
# 产物仅包含压缩归档 (tar.gz/zip) + SHA256SUMS，不含裸二进制。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="${REPO_ROOT}/src"
RELEASE_DIR="${REPO_ROOT}/release"
SCRIPTS_DIR="${REPO_ROOT}/scripts"

source "${SCRIPTS_DIR}/platforms.sh"

if ! find "${SRC_DIR}/proxy/webui_dist/assets" -maxdepth 1 -type f \( -name '*.js' -o -name '*.js.gz' \) -print -quit 2>/dev/null | grep -q .; then
  echo "error: src/proxy/webui_dist 缺少 WebUI JS assets，请先构建并嵌入前端" >&2
  exit 1
fi

# 确定版本号和构建元数据；正式发布由 release.sh 显式覆盖。
VERSION="${YIGATE_VERSION:-${1:-}}"
if [[ -z "$VERSION" ]]; then
    VERSION="$(bash "${SCRIPTS_DIR}/version.sh")"
fi

# 校验 WebUI 前端是否已构建并拷贝到 embed 目录
# webui_dist 需包含 assets/ 目录（真实前端产物），仅含占位 index.html 视为未集成前端。
if [[ ! -d "${SRC_DIR}/proxy/webui_dist/assets" ]]; then
    if [[ -n "${ALLOW_EMBED_FALLBACK:-}" ]]; then
        echo "warning: webui_dist/assets 缺失，将使用占位页构建（WebUI 不可用）。"
    else
        echo "error: webui_dist/assets 不存在，未集成前端产物。" >&2
        echo "       请先执行: make web-embed（或 node web/scripts/embed-dist.mjs web/dist src/proxy/webui_dist）" >&2
        echo "       或设置 ALLOW_EMBED_FALLBACK=1 允许降级构建。" >&2
        exit 1
    fi
fi

COMMIT="${YIGATE_COMMIT:-${CNB_COMMIT:-$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || echo unknown)}}"
BUILD_TIME="${YIGATE_BUILD_TIME:-${CNB_BUILD_START_TIME:-$(git -C "${REPO_ROOT}" log -1 --format=%cI 2>/dev/null || echo unknown)}}"

LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

echo "=== Building YiGate ${VERSION} (commit ${COMMIT:0:12}) ==="

# 准备 release 目录
rm -rf "${RELEASE_DIR}"
mkdir -p "${RELEASE_DIR}"
echo "$VERSION" > "${RELEASE_DIR}/.version"

# 校验并下载依赖
echo "--- Verifying dependencies ---"
(
    cd "${SRC_DIR}"
    GOPROXY=https://goproxy.cn,direct go mod download
    GOPROXY=https://goproxy.cn,direct go mod verify
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
            go build -trimpath -mod=readonly -ldflags "${LDFLAGS}" \
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

# 生成 SBOM；正式发布要求必须产出
if command -v syft >/dev/null 2>&1; then
    echo "--- Generating SBOM ---"
    syft dir "${SRC_DIR}" -o spdx-json > "${RELEASE_DIR}/yigate-${VERSION}.spdx.json"
elif [[ "${YIGATE_REQUIRE_SBOM:-0}" == "1" ]]; then
    echo "error: YIGATE_REQUIRE_SBOM=1 但未安装 syft" >&2
    exit 1
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
