#!/bin/bash
# yigate systemd 安装脚本
# 用法: ./install.sh [local|lan]
# 运行前请确保当前目录包含 yigate（二进制）和 yigate.service

set -e

INSTALL_DIR="/usr/local/bin"
TEMPLATE_DIR="/etc/yigate"
DATA_DIR="/var/lib/yigate/data"
LOG_DIR="/var/log/yigate"
SERVICE_FILE="/etc/systemd/system/yigate.service"

TEMPLATE="${1:-local}"

if [ "$EUID" -ne 0 ]; then
    echo "请使用 root 权限运行此脚本"
    exit 1
fi

if [ ! -f "yigate" ]; then
    echo "错误：当前目录缺少 yigate 二进制文件"
    exit 1
fi

if [ ! -f "yigate.service" ]; then
    echo "错误：当前目录缺少 yigate.service 服务文件"
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
if ! id -u yigate &>/dev/null; then
    useradd -r -s /usr/sbin/nologin -M yigate
fi

# 安装二进制
install -m 755 yigate "${INSTALL_DIR}/yigate"

# 安装模板到只读目录
mkdir -p "${TEMPLATE_DIR}"
install -m 644 "$TEMPLATE_FILE" "${TEMPLATE_DIR}/${TEMPLATE_FILE}"

# 创建可写运行目录
mkdir -p "${DATA_DIR}" "${LOG_DIR}"

# 首次安装时从模板初始化运行配置
if [ ! -f "${DATA_DIR}/config.yml" ]; then
    install -m 640 -o yigate -g yigate "$TEMPLATE_FILE" "${DATA_DIR}/config.yml"
    echo "运行配置已初始化: ${DATA_DIR}/config.yml"
else
    echo "运行配置已存在，跳过覆盖: ${DATA_DIR}/config.yml"
fi

# 确保目录归属运行用户
chown -R yigate:yigate "${DATA_DIR}" "${LOG_DIR}"

# 安装服务文件
install -m 644 yigate.service "${SERVICE_FILE}"

# 重载 systemd
systemctl daemon-reload
systemctl enable yigate

echo "yigate 已安装并启用（模板: ${TEMPLATE}）。"
echo "启动命令: systemctl start yigate"
echo "查看状态: systemctl status yigate"
echo "查看日志: journalctl -u yigate -f"
