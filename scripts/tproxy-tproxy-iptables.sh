#!/bin/bash
#
# k5pw 透明代理 iptables 配置示例（TPROXY 模式）
#
# TPROXY 模式：iptables 将匹配的流量通过 TPROXY target 交给本地代理，
# 代理通过 conn.LocalAddr() 获取原始目标地址。
# 此模式保留客户端源 IP，但需要设置 IP_TRANSPARENT socket option。
#
# 前置条件：
#   - Linux 内核支持 TPROXY（2.6.28+）
#   - 加载 xt_tproxy 模块
#   - 代理进程需要 CAP_NET_ADMIN 或 root 权限
#
# 用法：
#   ./tproxy-tproxy-iptables.sh <proxy_port> [network_interface]
#
# 示例：
#   ./tproxy-tproxy-iptables.sh 8891 eth0
#
# 清除规则：
#   ./tproxy-tproxy-iptables.sh --clear

PROXY_PORT="${1:-8891}"
IFACE="${2:-}"

if [ "$PROXY_PORT" = "--clear" ]; then
    REAL_PORT="${2:-8891}"
    echo "清除所有 k5pw TPROXY 规则..."
    iptables -t mangle -D PREROUTING -p tcp -m set --match-set k5pw_dst dst -j TPROXY --tproxy-mark 0x1/0x1 --on-port "$REAL_PORT" 2>/dev/null
    iptables -t mangle -D PREROUTING -p tcp -j MARK --set-mark 0x1/0x1 2>/dev/null
    ip rule del fwmark 0x1/0x1 lookup 100 2>/dev/null
    ip route del local 0.0.0.0/0 dev lo table 100 2>/dev/null
    ipset destroy k5pw_dst 2>/dev/null
    echo "已清除。"
    exit 0
fi

echo "=== k5pw 透明代理 iptables 配置（TPROXY 模式）==="
echo "代理端口: $PROXY_PORT"
echo "网络接口: ${IFACE:-（全部）}"
echo ""

# 1. 创建 ipset 用于目标地址匹配
ipset create k5pw_dst hash:net -exist
ipset add k5pw_dst 0.0.0.0/0 -exist

# 2. 设置策略路由：将 fwmark 0x1 的数据包路由到本地
ip rule add fwmark 0x1/0x1 lookup 100 2>/dev/null
ip route add local 0.0.0.0/0 dev lo table 100 2>/dev/null

# 3. mangle PREROUTING：标记数据包并交给 TPROXY
if [ -n "$IFACE" ]; then
    iptables -t mangle -A PREROUTING -i "$IFACE" -p tcp -m set --match-set k5pw_dst dst -j TPROXY --tproxy-mark 0x1/0x1 --on-port "$PROXY_PORT"
else
    iptables -t mangle -A PREROUTING -p tcp -m set --match-set k5pw_dst dst -j TPROXY --tproxy-mark 0x1/0x1 --on-port "$PROXY_PORT"
fi

echo "iptables 规则已添加。"
echo ""
echo "配置示例（config.yml）:"
echo "  listeners:"
echo "    - listen: \":$PROXY_PORT\""
echo "      tproxy:"
echo "        enabled: true"
echo "        mode: tproxy"
echo ""
echo "注意：TPROXY 模式需要代理进程以 root 或 CAP_NET_ADMIN 权限运行。"
echo ""
echo "清除规则："
echo "  $0 --clear $PROXY_PORT"
