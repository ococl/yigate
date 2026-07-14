#!/usr/bin/env bash
set -euo pipefail

# 检查 deploy/config/ 下的产品配置模板是否符合安全默认值。
# 退出码非零表示发现安全问题。

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG_DIR="${REPO_ROOT}/deploy/config"
status=0

for cfg in "${CONFIG_DIR}"/config.*.yml; do
  [[ -f "$cfg" ]] || continue
  name="$(basename "$cfg")"

  # 局域网模板不得同时出现非回环监听和空用户表
  if [[ "$name" == "config.lan.yml" ]]; then
    if grep -q 'listen:.*0\.0\.0\.0' "$cfg" && grep -q 'users: {}' "$cfg"; then
      echo "error: ${name} 非回环监听与空用户表同时出现" >&2
      status=1
    fi
    if ! grep -q 'management_auth_token:' "$cfg"; then
      echo "error: ${name} 局域网模板缺少 management_auth_token" >&2
      status=1
    fi
  fi

  # 本机模板不得监听非回环地址
  if [[ "$name" == "config.local.yml" ]]; then
    if grep -q 'listen:.*0\.0\.0\.0' "$cfg" || grep -q 'listen:.*\[' "$cfg"; then
      echo "error: ${name} 本机模板不得监听非回环地址" >&2
      status=1
    fi
  fi

  # 所有产品模板不得包含固定私网地址或真实凭据
  if grep -qE '(10|172\.(1[6-9]|2[0-9]|3[01])|192\.168)\.[0-9]+\.[0-9]+:[0-9]+' "$cfg" \
     && ! grep -q 'allowed_clients' "$cfg"; then
    echo "error: ${name} 包含疑似固定私网上游地址" >&2
    status=1
  fi
done

if [[ $status -eq 0 ]]; then
  echo "OK: 产品配置模板安全检查通过"
fi
exit $status
