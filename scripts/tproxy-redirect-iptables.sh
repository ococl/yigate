#!/bin/bash
#
# yigate 透明代理 iptables 配置示例（REDIRECT 模式）
#
# REDIRECT 模式：iptables 将匹配的流量重定向到本地代理端口，
# 代理通过 SO_ORIGINAL_DST 获取原始目标地址。
# 此模式简单易用，但不保留客户端源 IP。
#
# 用法：
#   ./tproxy-redirect-iptables.sh <proxy_port> [network_interface]
#
# 示例：
#   ./tproxy-redirect-iptables.sh 8891 eth0
#
# 清除规则：
#   ./tproxy-redirect-iptables.sh --clear

PROXY_PORT="${1:-8891}"
IFACE="${2:-}"

if [ "$PROXY_PORT" = "--clear" ]; then
    echo "清除所有 yigate TPROXY 规则..."
    iptables -t nat -D PREROUTING -p tcp -m set --match-set yigate_dst dst -j REDIRECT --to-ports "${2:-8891}" 2>/dev/null
    iptables -t nat -D OUTPUT -p tcp -m owner --uid-owner 0 -j RETURN 2>/dev/null
    iptables -t nat -D OUTPUT -p tcp -m set --match-set yigate_dst dst -j REDIRECT --to-ports "${2:-8891}" 2>/dev/null
    ipset destroy yigate_dst 2>/dev/null
    echo "已清除。"
    exit 0
fi

echo "=== yigate 透明代理 iptables 配置（REDIRECT 模式）==="
echo "代理端口: $PROXY_PORT"
echo "网络接口: ${IFACE:-（全部）}"
echo ""

# 1. 创建 ipset 用于目标地址匹配（可选，也可直接用 CIDR）
ipset create yigate_dst hash:net -exist
# 示例：将所有流量重定向到代理（生产环境请按需调整）
ipset add yigate_dst 0.0.0.0/0 -exist

# 2. PREROUTING 链：将来自局域网的流量重定向到代理
if [ -n "$IFACE" ]; then
    iptables -t nat -A PREROUTING -i "$IFACE" -p tcp -m set --match-set yigate_dst dst -j REDIRECT --to-ports "$PROXY_PORT"
else
    iptables -t nat -A PREROUTING -p tcp -m set --match-set yigate_dst dst -j REDIRECT --to-ports "$PROXY_PORT"
fi

# 3. OUTPUT 链：将本机发出的流量也重定向到代理（排除 root 用户以避免循环）
iptables -t nat -A OUTPUT -p tcp -m owner --uid-owner 0 -j RETURN
iptables -t nat -A OUTPUT -p tcp -m set --match-set yigate_dst dst -j REDIRECT --to-ports "$PROXY_PORT"

echo "iptables 规则已添加。"
echo ""
echo "配置示例（config.yml）:"
echo "  listeners:"
echo "    - listen: \":$PROXY_PORT\""
echo "      tproxy:"
echo "        enabled: true"
echo "        mode: redirect"
echo ""
echo "清除规则："
echo "  $0 --clear $PROXY_PORT"
