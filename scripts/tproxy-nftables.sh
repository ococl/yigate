#!/bin/bash
#
# k5pw 透明代理 nftables 配置示例
#
# 支持 REDIRECT 和 TPROXY 两种模式。
# nftables 是 iptables 的现代替代品，语法更清晰，性能更好。
#
# 用法：
#   ./tproxy-nftables.sh <mode> <proxy_port> [network_interface]
#
# 示例：
#   ./tproxy-nftables.sh redirect 8891 eth0
#   ./tproxy-nftables.sh tproxy 8891 eth0
#
# 清除规则：
#   ./tproxy-nftables.sh clear

MODE="${1:-redirect}"
PROXY_PORT="${2:-8891}"
IFACE="${3:-}"

if [ "$MODE" = "clear" ]; then
    echo "清除 k5pw nftables 规则..."
    nft delete table inet k5pw_tproxy 2>/dev/null
    ip rule del fwmark 0x1/0x1 lookup 100 2>/dev/null
    ip route del local 0.0.0.0/0 dev lo table 100 2>/dev/null
    echo "已清除。"
    exit 0
fi

echo "=== k5pw 透明代理 nftables 配置（$MODE 模式）==="
echo "代理端口: $PROXY_PORT"
echo "网络接口: ${IFACE:-（全部）}"
echo ""

# 清除旧规则
nft delete table inet k5pw_tproxy 2>/dev/null

if [ "$MODE" = "tproxy" ]; then
    # TPROXY 模式：需要策略路由
    ip rule add fwmark 0x1/0x1 lookup 100 2>/dev/null
    ip route add local 0.0.0.0/0 dev lo table 100 2>/dev/null

    nft -f - <<EOF
table inet k5pw_tproxy {
    chain prerouting {
        type nat hook prerouting priority mangle;
        ${IFACE:+iifname "$IFACE"} tcp dport != $PROXY_PORT tproxy to :$PROXY_PORT mark set 0x1
    }
}
EOF

    echo "nftables TPROXY 规则已添加。"
    echo ""
    echo "配置示例（config.yml）:"
    echo "  listeners:"
    echo "    - listen: \":$PROXY_PORT\""
    echo "      tproxy:"
    echo "        enabled: true"
    echo "        mode: tproxy"

elif [ "$MODE" = "redirect" ]; then
    nft -f - <<EOF
table inet k5pw_tproxy {
    chain prerouting {
        type nat hook prerouting priority dstnat;
        ${IFACE:+iifname "$IFACE"} tcp dport != $PROXY_PORT redirect to :$PROXY_PORT
    }
    chain output {
        type nat hook output priority dstnat;
        meta skuid 0 return
        tcp dport != $PROXY_PORT redirect to :$PROXY_PORT
    }
}
EOF

    echo "nftables REDIRECT 规则已添加。"
    echo ""
    echo "配置示例（config.yml）:"
    echo "  listeners:"
    echo "    - listen: \":$PROXY_PORT\""
    echo "      tproxy:"
    echo "        enabled: true"
    echo "        mode: redirect"
fi

echo ""
echo "清除规则："
echo "  $0 clear"
