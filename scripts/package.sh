#!/usr/bin/env bash
set -euo pipefail

# package.sh — 通用平台打包脚本。
# 将裸二进制打包为压缩归档，包含平台适用的附属文件。
#
# 用法: package.sh <version> <platform>
# 例: package.sh 1.2.0 linux/amd64
#     package.sh 1.2.0 windows/amd64
#
# 产物: release/yigate-<version>-<suffix>.tar.gz  (Unix)
#       release/yigate-<version>-<suffix>.zip     (Windows)

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
DEPLOY_DIR="${REPO_ROOT}/deploy"
SCRIPTS_DIR="${REPO_ROOT}/scripts"

source "${SCRIPTS_DIR}/platforms.sh"

VERSION="$1"
PLATFORM="$2"
GOOS="$(echo "$PLATFORM" | cut -d/ -f1)"

SUFFIX="$(normalize_platform "$PLATFORM")"
BINARY="$(output_name "${VERSION}" "${PLATFORM}")"
BINARY_PATH="${RELEASE_DIR}/${BINARY}"
PKG_NAME="yigate-${VERSION}-${SUFFIX}"
STAGING_DIR="${RELEASE_DIR}/.pkg-${PKG_NAME}"

if [[ ! -f "${BINARY_PATH}" ]]; then
    echo "error: binary not found: ${BINARY_PATH}" >&2
    exit 1
fi

# 组装临时目录
rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}"

# 复制二进制
if [[ "$GOOS" == "windows" ]]; then
    cp "${BINARY_PATH}" "${STAGING_DIR}/yigate.exe"
else
    cp "${BINARY_PATH}" "${STAGING_DIR}/yigate"
    chmod +x "${STAGING_DIR}/yigate"
fi

# 通用文件
cp "${REPO_ROOT}/LICENSE" "${STAGING_DIR}/LICENSE"

# Linux 专用: systemd 服务、安装脚本、配置模板
if [[ "$GOOS" == "linux" ]]; then
    cp "${DEPLOY_DIR}/config/config.local.yml" "${STAGING_DIR}/config.local.yml"
    cp "${DEPLOY_DIR}/config/config.lan.yml" "${STAGING_DIR}/config.lan.yml"
    cp "${DEPLOY_DIR}/systemd/yigate.service" "${STAGING_DIR}/yigate.service"
    cp "${DEPLOY_DIR}/systemd/install.sh" "${STAGING_DIR}/install.sh"
    chmod +x "${STAGING_DIR}/install.sh"
fi

# 打包: Windows 用 zip (最大压缩), 其他用 tar.gz (gzip -9)
if [[ "$GOOS" == "windows" ]]; then
    ARCHIVE="${RELEASE_DIR}/${PKG_NAME}.zip"
    (cd "${STAGING_DIR}" && zip -9 -q -r "${ARCHIVE}" .)
else
    ARCHIVE="${RELEASE_DIR}/${PKG_NAME}.tar.gz"
    GZIP=-9 tar -czf "${ARCHIVE}" -C "${STAGING_DIR}" .
fi

rm -rf "${STAGING_DIR}"
echo "Package: ${ARCHIVE}"
