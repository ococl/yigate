#!/bin/bash
# yigate systemd 安装脚本
# 用法: ./install.sh [local|lan]
# 运行前请确保当前目录包含 yigate（二进制）和 yigate.service
#
# lan 模式会生成随机代理密码和管理令牌，写入
# /etc/yigate/initial-credentials（0600），不会使用仓库中的固定值。

set -euo pipefail

INSTALL_DIR="/usr/local/bin"
TEMPLATE_DIR="/etc/yigate"
DATA_DIR="/var/lib/yigate/data"
LOG_DIR="/var/log/yigate"
SERVICE_FILE="/etc/systemd/system/yigate.service"
CREDENTIALS_FILE="/etc/yigate/initial-credentials"

TEMPLATE="${1:-local}"
TMP_CONFIG=""

cleanup() {
    [[ -n "$TMP_CONFIG" ]] && rm -f "$TMP_CONFIG"
}
trap cleanup EXIT

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

generate_secret() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -base64 24 | tr -d '\n\r'
    else
        head -c 32 /dev/urandom | base64 | tr -d '\n\r'
    fi
}

render_lan_config() {
    local output="$1"
    local proxy_password="${YIGATE_PROXY_PASSWORD:-$(generate_secret)}"
    local admin_token="${YIGATE_ADMIN_TOKEN:-$(generate_secret)}"

    if printf '%s' "${proxy_password}" | grep -q '[^A-Za-z0-9._~+/=-]' \
       || printf '%s' "${admin_token}" | grep -q '[^A-Za-z0-9._~+/=-]'; then
        echo "错误：YIGATE_PROXY_PASSWORD / YIGATE_ADMIN_TOKEN 含不支持的字符" >&2
        exit 1
    fi

    sed \
        -e "s|__REPLACE_PROXY_PASSWORD__|${proxy_password}|g" \
        -e "s|__REPLACE_MANAGEMENT_TOKEN__|${admin_token}|g" \
        "$TEMPLATE_FILE" > "$output"

    if grep -q '__REPLACE_' "$output"; then
        echo "错误：LAN 配置仍含未替换占位符" >&2
        exit 1
    fi

    umask 077
    cat > "$CREDENTIALS_FILE" <<EOF
proxy_username=proxy
proxy_password=${proxy_password}
management_auth_token=${admin_token}
EOF
    chmod 0600 "$CREDENTIALS_FILE"
    echo "已生成随机 LAN 凭据: ${CREDENTIALS_FILE}"
}

# 创建运行用户
if ! id -u yigate &>/dev/null; then
    useradd -r -s /usr/sbin/nologin -d /var/lib/yigate -M yigate
fi

install -m 755 yigate "${INSTALL_DIR}/yigate"

mkdir -p "${TEMPLATE_DIR}"
install -m 644 "$TEMPLATE_FILE" "${TEMPLATE_DIR}/${TEMPLATE_FILE}"

install -d -m 0750 -o yigate -g yigate "${DATA_DIR}" "${LOG_DIR}"

# 首次安装时初始化运行配置；LAN 模式绝不落固定凭据。
if [ ! -f "${DATA_DIR}/config.yml" ]; then
    if [ "$TEMPLATE" = "lan" ]; then
        TMP_CONFIG="$(mktemp)"
        render_lan_config "$TMP_CONFIG"
        install -m 640 -o yigate -g yigate "$TMP_CONFIG" "${DATA_DIR}/config.yml"
    else
        install -m 640 -o yigate -g yigate "$TEMPLATE_FILE" "${DATA_DIR}/config.yml"
    fi
    echo "运行配置已初始化: ${DATA_DIR}/config.yml"
else
    echo "运行配置已存在，跳过覆盖: ${DATA_DIR}/config.yml"
    if [ "$TEMPLATE" = "lan" ]; then
        echo "注意：如配置仍含 __REPLACE_*__ 占位符，服务会拒绝启动，请先替换为强凭据。"
    fi
fi

chown -R yigate:ygate "${DATA_DIR}" "${LOG_DIR}"

install -m 644 yigate.service "${SERVICE_FILE}"

systemctl daemon-reload
systemctl enable yigate

echo "yigate 已安装并启用（模板: ${TEMPLATE}）。"
if [ -f "$CREDENTIALS_FILE" ]; then
    echo "初始凭据: ${CREDENTIALS_FILE}（仅 root 可读，请妥善保存并删除）"
fi
echo "启动命令: systemctl start yigate"
echo "查看状态: systemctl status yigate"
echo "查看日志: journalctl -u yigate -f"
