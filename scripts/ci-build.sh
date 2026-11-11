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
# 固定 Syft 版本和镜像 digest，避免 SBOM 生成工具随上游 main 漂移。
SYFT_IMAGE="${YIGATE_SYFT_IMAGE:-docker.io/anchore/syft:v1.57.0@sha256:3c70e6d5979364cefdc22b51617a78fd264574d3bd6b4f84a351204147025e2a}"

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
# 26 个平台串行编译是整条流水线最慢的一段（每个平台都要重编全部依赖），
# 这里按 CPU 数并发；单个平台仍是完整的 go build，产物与串行完全一致。
PLATFORMS=($(selected_platforms))
JOBS="${YIGATE_BUILD_JOBS:-$(nproc 2>/dev/null || echo 4)}"
echo "--- Cross-compiling ${#PLATFORMS[@]} platforms (jobs=${JOBS}) ---"

# 并行执行 fn <item>，任一失败即返回非零（其余任务仍会跑完，便于一次看到全部错误）。
run_parallel() {
    local fn="$1"
    shift
    local failed=0 running=0 item
    for item in "$@"; do
        "${fn}" "${item}" &
        running=$((running + 1))
        if (( running >= JOBS )); then
            wait -n || { echo "error: 并行任务失败: ${item}" >&2; failed=1; }
            running=$((running - 1))
        fi
    done
    wait || failed=1
    return "${failed}"
}

build_platform() {
    local platform="$1"
    local name env_str
    name="$(output_name "${VERSION}" "${platform}")"
    env_str="$(build_env "${platform}")"
    echo "  Building ${name}..."
    (
        cd "${SRC_DIR}"
        env ${env_str} CGO_ENABLED=0 GOPROXY=https://goproxy.cn,direct \
            go build -trimpath -buildvcs=false -mod=readonly -ldflags "${LDFLAGS}" \
            -o "${RELEASE_DIR}/${name}" .
    ) || { echo "error: 编译失败: ${platform}" >&2; return 1; }
}

run_parallel build_platform "${PLATFORMS[@]}" || exit 1

# 全平台打包
# gzip -9 压缩同样吃 CPU，串行打包会白白多花一分钟，这里一并并行。
package_platform() {
    "${SCRIPTS_DIR}/package.sh" "${VERSION}" "$1"
}

echo "--- Packaging ${#PLATFORMS[@]} archives (jobs=${JOBS}) ---"
run_parallel package_platform "${PLATFORMS[@]}" || exit 1

# 删除裸二进制，仅保留压缩归档。
# linux/amd64 留一份到 build/，供镜像构建直接复用（二进制已内嵌前端与版本信息），
# 省掉 Dockerfile 里重复的 npm ci 与 go build。
echo "--- Removing raw binaries ---"
mkdir -p "${REPO_ROOT}/build"
for platform in "${PLATFORMS[@]}"; do
    name="$(output_name "${VERSION}" "${platform}")"
    if [[ "${platform}" == "linux/amd64" ]]; then
        mv -f "${RELEASE_DIR}/${name}" "${REPO_ROOT}/build/yigate-linux-amd64"
        continue
    fi
    rm -f "${RELEASE_DIR}/${name}"
done

# 生成 SBOM；优先用本机 syft，否则使用固定 digest 的 Syft 容器。
# 正式发布必须产出；本地普通构建在工具不可用时允许跳过。
generate_sbom() {
    local output="$1"
    if command -v syft >/dev/null 2>&1; then
        # syft >= 1.x 收敛为 `syft scan <source>`，目录需写成 dir:<path> 形式。
        if syft "dir:${SRC_DIR}" -o spdx-json > "${output}"; then
            return 0
        fi
        rm -f "${output}"
        return 1
    fi
    if ! command -v docker >/dev/null 2>&1 || ! docker version >/dev/null 2>&1; then
        return 1
    fi

    local cid
    cid="$(docker create "${SYFT_IMAGE}" dir:/src -o spdx-json)" || return 1
    if ! docker cp "${SRC_DIR}/." "${cid}:/src" >/dev/null; then
        docker rm -f "${cid}" >/dev/null 2>&1 || true
        rm -f "${output}"
        return 1
    fi
    if ! docker start -a "${cid}" > "${output}"; then
        docker rm -f "${cid}" >/dev/null 2>&1 || true
        rm -f "${output}"
        return 1
    fi
    docker rm -f "${cid}" >/dev/null 2>&1 || true
    return 0
}

if generate_sbom "${RELEASE_DIR}/yigate-${VERSION}.spdx.json"; then
    echo "--- Generated SBOM ---"
elif [[ "${YIGATE_REQUIRE_SBOM:-0}" == "1" ]]; then
    echo "error: YIGATE_REQUIRE_SBOM=1 但 SBOM 生成失败（syft/Docker 不可用或执行失败）" >&2
    exit 1
else
    echo "--- Skipping SBOM (syft and Docker unavailable) ---"
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
