#!/usr/bin/env bash
set -euo pipefail

# 生成 Linux 安装包归档。
# 用法: package-linux.sh <version> <platform-suffix>
# 例: package-linux.sh 1.2.0 linux-amd64

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
DEPLOY_DIR="${REPO_ROOT}/deploy"

VERSION="$1"
SUFFIX="$2"
PKG_NAME="yigate-${VERSION}-${SUFFIX}"
STAGING_DIR="${RELEASE_DIR}/.pkg-${PKG_NAME}"
ARCHIVE="${RELEASE_DIR}/${PKG_NAME}.tar.gz"

# 组装临时目录
rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}"

cp "${RELEASE_DIR}/yigate-${VERSION}-${SUFFIX}" "${STAGING_DIR}/yigate"
cp "${DEPLOY_DIR}/config/config.local.yml" "${STAGING_DIR}/config.local.yml"
cp "${DEPLOY_DIR}/config/config.lan.yml" "${STAGING_DIR}/config.lan.yml"
cp "${DEPLOY_DIR}/systemd/yigate.service" "${STAGING_DIR}/yigate.service"
cp "${DEPLOY_DIR}/systemd/install.sh" "${STAGING_DIR}/install.sh"
cp "${REPO_ROOT}/LICENSE" "${STAGING_DIR}/LICENSE"
chmod +x "${STAGING_DIR}/install.sh"

# 打包
tar -czf "${ARCHIVE}" -C "${STAGING_DIR}" .
rm -rf "${STAGING_DIR}"

echo "Package: ${ARCHIVE}"
