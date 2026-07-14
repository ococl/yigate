#!/bin/bash
# k5pw systemd 安装脚本
# 用法: ./install.sh [local|lan]
# 运行前请确保当前目录包含 k5pw（二进制）和 k5pw.service

set -e

INSTALL_DIR="/usr/local/bin"
TEMPLATE_DIR="/etc/k5pw"
CONFIG_DIR="/var/lib/k5pw/config"
DATA_DIR="/var/lib/k5pw/data"
LOG_DIR="/var/log/k5pw"
SERVICE_FILE="/etc/systemd/system/k5pw.service"

TEMPLATE="${1:-local}"

if [ "$EUID" -ne 0 ]; then
    echo "请使用 root 权限运行此脚本"
    exit 1
fi

if [ ! -f "k5pw" ]; then
    echo "错误：当前目录缺少 k5pw 二进制文件"
    exit 1
fi

if [ ! -f "k5pw.service" ]; then
    echo "错误：当前目录缺少 k5pw.service 服务文件"
    exit 1
fi

# 选择模板
case "$TEMPLATE" in
    local)
        TEMPLATE_FILE="config.local.yml"
        ;;
    lan)
        TEMPLATE_FILE="config.lan.yml"
        ;;
    *)
        echo "用法: $0 [local|lan]"
        exit 1
        ;;
esac

if [ ! -f "$TEMPLATE_FILE" ]; then
    echo "错误：当前目录缺少 $TEMPLATE_FILE 模板文件"
    exit 1
fi

# 创建运行用户
if ! id -u k5pw &>/dev/null; then
    useradd -r -s /usr/sbin/nologin -M k5pw
fi

# 安装二进制
install -m 755 k5pw "${INSTALL_DIR}/k5pw"

# 安装模板到只读目录
mkdir -p "${TEMPLATE_DIR}"
install -m 644 "$TEMPLATE_FILE" "${TEMPLATE_DIR}/${TEMPLATE_FILE}"

# 创建可写运行目录
mkdir -p "${CONFIG_DIR}" "${DATA_DIR}" "${LOG_DIR}"

# 首次安装时从模板初始化运行配置
if [ ! -f "${CONFIG_DIR}/config.yml" ]; then
    install -m 640 -o k5pw -g k5pw "$TEMPLATE_FILE" "${CONFIG_DIR}/config.yml"
    echo "运行配置已初始化: ${CONFIG_DIR}/config.yml"
else
    echo "运行配置已存在，跳过覆盖: ${CONFIG_DIR}/config.yml"
fi

# 确保目录归属运行用户
chown -R k5pw:k5pw "${CONFIG_DIR}" "${DATA_DIR}" "${LOG_DIR}"

# 安装服务文件
install -m 644 k5pw.service "${SERVICE_FILE}"

# 重载 systemd
systemctl daemon-reload
systemctl enable k5pw

echo "k5pw 已安装并启用（模板: ${TEMPLATE}）。"
echo "启动命令: systemctl start k5pw"
echo "查看状态: systemctl status k5pw"
echo "查看日志: journalctl -u k5pw -f"
