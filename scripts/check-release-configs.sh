#!/usr/bin/env bash
set -euo pipefail

# 检查随发布包/容器提供的配置模板是否符合安全默认值。
# 退出码非零表示发现安全问题。
#
# 注意：__REPLACE_*__ 是安装期占位符，由 install.sh 生成随机凭据；
# 服务端也会拒绝启动含占位符的凭据，避免模板被原样使用。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
status=0

check_config() {
  local cfg="$1"
  local name
  name="$(basename "$cfg")"

  # 禁止历史遗留的固定口令/令牌进入发布模板
  if grep -Eqi 'change-me|local-admin-token|password123|admin123' "$cfg"; then
    echo "error: ${cfg} 包含固定占位凭据" >&2
    status=1
  fi

  # 产品模板中的本机模式必须绑定回环地址
  if [[ "$name" == "config.local.yml" && "$cfg" == *"/deploy/config/"* ]]; then
    if grep -Eq 'listen:[[:space:]]*.*(0\.0\.0\.0|\[::\])' "$cfg"; then
      echo "error: ${cfg} 本机模板不得监听非回环地址" >&2
      status=1
    fi
  fi

  # LAN 模板必须启用认证，且管理 Token 不能为空
  if [[ "$name" == "config.lan.yml" ]]; then
    if grep -Eq '^[[:space:]]*users:[[:space:]]*\{\}' "$cfg"; then
      echo "error: ${cfg} 局域网模板不允许空用户表" >&2
      status=1
    fi
    if ! grep -qE '^[[:space:]]*users:[[:space:]]*$' "$cfg"; then
      echo "error: ${cfg} 局域网模板缺少 users 配置" >&2
      status=1
    elif ! grep -A1 -E '^[[:space:]]*users:[[:space:]]*$' "$cfg" | tail -n 1 | grep -qE '^[[:space:]]+[^[:space:]]'; then
      echo "error: ${cfg} 局域网模板 users 不能为空" >&2
      status=1
    fi
    if ! grep -qE '^[[:space:]]*management_auth_token:[[:space:]]*[^[:space:]]' "$cfg"; then
      echo "error: ${cfg} 局域网模板缺少非空 management_auth_token" >&2
      status=1
    fi
  fi

  # 所有模板不得包含固定私网上游地址；允许显式配置 allowed_clients 的 LAN 模板
  if grep -qE '(10|172\.(1[6-9]|2[0-9]|3[01])|192\.168)\.[0-9]+\.[0-9]+:[0-9]+' "$cfg" \
     && ! grep -q 'allowed_clients' "$cfg"; then
    echo "error: ${cfg} 包含疑似固定私网上游地址" >&2
    status=1
  fi
}

for dir in "${REPO_ROOT}/deploy/config" "${REPO_ROOT}/deploy/docker"; do
  for cfg in "${dir}"/config.*.yml; do
    [[ -f "$cfg" ]] || continue
    check_config "$cfg"
  done
done

if [[ $status -eq 0 ]]; then
  echo "OK: 发布配置模板安全检查通过"
fi
exit $status
