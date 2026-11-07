#!/usr/bin/env bash
# 服务管理生命周期真机（本机 systemd）测试脚本
#
# 在 root + systemd 环境中，真实演练 yigate 的服务管理生命周期：
#   install -> status(stopped) -> start -> status(running) -> health -> stop -> status(stopped)
#   -> start -> status(running) -> restart -> status(running) -> uninstall
#
# 每一步均使用三方交叉验证：
#   1) `yigate -config <cfg> status` 命令输出
#   2) `systemctl is-active/is-enabled yigate` 系统状态
#   3) 管理端口 /health 端点可达性（仅运行态）
# 并在卸载前读取真实生成的 systemd 单元文件，断言跨平台重启策略已注入。
#
# 用法：
#   sudo bash scripts/test_service_lifecycle.sh
#
# 退出码：0 全部通过，非 0 存在失败（异常时自动卸载并清理）。

set -o pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="$REPO_ROOT/src"
UNIT_NAME="yigate"
TMP_DIR="$(mktemp -d /tmp/yigate-svc-test.XXXXXX)"
BIN="$TMP_DIR/yigate"
CFG="$TMP_DIR/config.yml"
LOG="$TMP_DIR/yigate.log"

LISTEN_PORT=18971
MGMT_PORT=18970
MGMT_URL="http://127.0.0.1:${MGMT_PORT}"
MAX_WAIT=15

PASS=0
FAIL=0

# ---------------------------------------------------------------------------
# 工具函数
# ---------------------------------------------------------------------------
log()  { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; PASS=$((PASS+1)); }
fail() { printf '  \033[31mFAIL\033[0m %s :: %s\n' "$1" "$2"; FAIL=$((FAIL+1)); }

# 等待管理端口健康（运行态）
wait_health() {
  local waited=0
  while [ "$waited" -lt "$MAX_WAIT" ]; do
    if curl -fsS --max-time 2 "${MGMT_URL}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    waited=$((waited+1))
  done
  return 1
}

# 等待管理端口不可达（停止态）
wait_down() {
  local waited=0
  while [ "$waited" -lt "$MAX_WAIT" ]; do
    if ! curl -fsS --max-time 2 "${MGMT_URL}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    waited=$((waited+1))
  done
  return 1
}

# 查询 yigate status 命令输出（running/stopped/unknown）
kv_status() {
  "$BIN" -config "$CFG" status 2>/dev/null | sed -n 's/.*service: //p'
}

# 系统激活状态
sys_active() { systemctl is-active "$UNIT_NAME" 2>/dev/null; }
sys_enabled() { systemctl is-enabled "$UNIT_NAME" 2>/dev/null; }

# 断言运行态
expect_running() {
  local label="$1" ok=1
  local ks sys
  ks="$(kv_status)"; sys="$(sys_active)"
  if [ "$ks" = "running" ]; then pass "$label: yigate status=running"; else fail "$label" "yigate status='$ks'"; ok=0; fi
  if [ "$sys" = "active" ];  then pass "$label: systemctl is-active=active"; else fail "$label" "systemctl is-active='$sys'"; ok=0; fi
  if wait_health;            then pass "$label: /health reachable"; else fail "$label" "/health unreachable"; ok=0; fi
  [ "$ok" -eq 1 ]
}

# 断言停止态
expect_stopped() {
  local label="$1" ok=1
  local ks sys
  ks="$(kv_status)"; sys="$(sys_active)"
  if [ "$ks" = "stopped" ]; then pass "$label: yigate status=stopped"; else fail "$label" "yigate status='$ks'"; ok=0; fi
  if [ "$sys" = "inactive" ] || [ "$sys" = "unknown" ]; then pass "$label: systemctl is-active=$sys"; else fail "$label" "systemctl is-active='$sys'"; ok=0; fi
  if wait_down;              then pass "$label: /health unreachable"; else fail "$label" "/health still reachable"; ok=0; fi
  [ "$ok" -eq 1 ]
}

# 断言已注册但未运行（install 后预期状态）
expect_installed() {
  local label="$1" ok=1
  local en ks
  en="$(sys_enabled)"; ks="$(kv_status)"
  if [ "$en" = "enabled" ]; then pass "$label: systemctl is-enabled=enabled"; else fail "$label" "systemctl is-enabled='$en'"; ok=0; fi
  if [ "$ks" != "running" ]; then pass "$label: yigate status=$ks (未运行，符合预期)"; else fail "$label" "install 后不应自动运行"; ok=0; fi
  if wait_down;              then pass "$label: /health 不可达（未运行）"; else fail "$label" "/health 竟可达"; ok=0; fi
  [ "$ok" -eq 1 ]
}

# ---------------------------------------------------------------------------
# 清理（异常退出也必须卸载并移除临时文件）
# ---------------------------------------------------------------------------
cleanup() {
  log "清理中..."
  # 直接使用 systemctl 解除并移除单元，避免依赖被测二进制是否仍存在。
  systemctl stop "$UNIT_NAME"    >/dev/null 2>&1 || true
  systemctl disable "$UNIT_NAME" >/dev/null 2>&1 || true
  rm -f "/etc/systemd/system/${UNIT_NAME}.service"
  systemctl daemon-reload        >/dev/null 2>&1 || true
  systemctl reset-failed "$UNIT_NAME" >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
  log "已清理临时目录 $TMP_DIR"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# 前置检查
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
  log "错误：本测试需要 root 权限以注册 systemd 服务（请 sudo 运行）"
  exit 2
fi
if [ ! -d /run/systemd/system ]; then
  log "错误：当前环境未运行 systemd（/run/systemd/system 不存在），无法做真机服务测试"
  exit 3
fi

# ---------------------------------------------------------------------------
# 1. 构建二进制
# ---------------------------------------------------------------------------
log "== 1. 构建 yigate 二进制 =="
if (cd "$SRC_DIR" && GOPROXY=https://goproxy.cn,direct go build -o "$BIN" .) 2>"$TMP_DIR/build.log"; then
  pass "go build 成功 -> $BIN"
else
  fail "go build" "$(cat "$TMP_DIR/build.log")"
  exit 4
fi

# ---------------------------------------------------------------------------
# 2. 生成隔离测试配置（监听回环，独立端口，独立日志）
# ---------------------------------------------------------------------------
log "== 2. 生成测试配置 =="
cat > "$CFG" <<EOF
listeners:
  - listen: "127.0.0.1:${LISTEN_PORT}"
management: "127.0.0.1:${MGMT_PORT}"
log:
  level: info
  format: json
  output: file
  file: "${LOG}"
  rotation: size
  max_size: 10
  max_age: 1
  max_backups: 1
  compress: false
upstreams:
  - name: dummy
    url: "socks5h://127.0.0.1:1"
    types: ["socks5"]
    priority: 1
    weight: 1
routing:
  final: direct
EOF
pass "配置已写入 $CFG (listen=:$LISTEN_PORT mgmt=:$MGMT_PORT)"

# ---------------------------------------------------------------------------
# 3. install（kardianos 在 systemd 下 install 会自动 enable+start）
# ---------------------------------------------------------------------------
log "== 3. install 服务 =="
if out=$("$BIN" -config "$CFG" install 2>&1); then
  pass "install 成功"
  log "    $out"
else
  fail "install" "$out"
  exit 5
fi

sleep 2
log "    [install 后] yigate status=$(kv_status) | systemctl is-active=$(sys_active) | enabled=$(sys_enabled)"
expect_installed "install后" || exit 5

# ---------------------------------------------------------------------------
# 4. 校验真实生成的 systemd 单元文件注入了跨平台重启策略
# ---------------------------------------------------------------------------
log "== 4. 校验 systemd 单元文件 (Restart 策略) =="
UNIT_FILE="/etc/systemd/system/${UNIT_NAME}.service"
if [ ! -f "$UNIT_FILE" ]; then
  fail "单元文件" "$UNIT_FILE 不存在"
  exit 5
fi
pass "单元文件存在: $UNIT_FILE"
assert_in_unit() {
  local key="$1" want="$2" val
  val="$(grep -E "^${key}=" "$UNIT_FILE" | tail -1 | cut -d= -f2-)"
  if [ "$val" = "$want" ]; then pass "单元 $key=$val"; else fail "单元 $key" "期望 '$want' 实际 '$val'"; fi
}
assert_in_unit "Restart" "always"
assert_in_unit "RestartSec" "10"
assert_in_unit "StartLimitBurst" "64"
if grep -q "SuccessExitStatus=1 2 8 SIGKILL" "$UNIT_FILE"; then pass "单元 SuccessExitStatus=1 2 8 SIGKILL"; else fail "单元 SuccessExitStatus" "缺失"; fi
if grep -qF "$CFG" "$UNIT_FILE" && grep -Eq "^ExecStart=.*service" "$UNIT_FILE"; then pass "单元 ExecStart 指向测试配置 ($(grep '^ExecStart=' "$UNIT_FILE"))"; else fail "单元 ExecStart" "未包含测试配置路径 ($(grep '^ExecStart=' "$UNIT_FILE"))"; fi

# ---------------------------------------------------------------------------
# 5. start -> 运行态
# ---------------------------------------------------------------------------
log "== 5. start 服务 =="
if out=$("$BIN" -config "$CFG" start 2>&1); then
  pass "start 成功"
else
  fail "start" "$out"
fi
sleep 2
log "    [start 后] yigate status=$(kv_status) | systemctl is-active=$(sys_active)"
expect_running "start后" || true

# ---------------------------------------------------------------------------
# 6. restart -> 运行态（并验证进程号变化）
# ---------------------------------------------------------------------------
log "== 6. restart 服务 =="
old_pid="$(systemctl show -p MainPID --value "$UNIT_NAME" 2>/dev/null)"
if out=$("$BIN" -config "$CFG" restart 2>&1); then
  pass "restart 成功"
else
  fail "restart" "$out"
fi
sleep 3
new_pid="$(systemctl show -p MainPID --value "$UNIT_NAME" 2>/dev/null)"
log "    restart 前 MainPID=$old_pid 后 MainPID=$new_pid"
if [ -n "$new_pid" ] && [ "$new_pid" != "$old_pid" ] && [ "$new_pid" != "0" ]; then
  pass "restart 后主进程已重建 (PID 变化)"
else
  fail "restart PID" "PID 未变化 (old=$old_pid new=$new_pid)"
fi
log "    [restart 后] yigate status=$(kv_status) | systemctl is-active=$(sys_active)"
expect_running "restart后" || true

# ---------------------------------------------------------------------------
# 7. 管理端点额外校验（运行态可用 status / metrics / health）
# ---------------------------------------------------------------------------
log "== 7. 管理端点 (health/status/metrics) =="
if curl -fsS --max-time 3 "${MGMT_URL}/health"          >/dev/null 2>&1; then pass "/health 200";          else fail "/health" "不可达"; fi
if curl -fsS --max-time 3 "${MGMT_URL}/api/v1/status"   >/dev/null 2>&1; then pass "/api/v1/status 200";   else fail "/api/v1/status" "不可达"; fi
if curl -fsS --max-time 3 "${MGMT_URL}/metrics"         >/dev/null 2>&1; then pass "/metrics 200";          else fail "/metrics" "不可达"; fi

# ---------------------------------------------------------------------------
# 8. stop -> 停止态
# ---------------------------------------------------------------------------
log "== 8. stop 服务 =="
if out=$("$BIN" -config "$CFG" stop 2>&1); then
  pass "stop 成功"
else
  fail "stop" "$out"
fi
sleep 2
log "    [stop 后] yigate status=$(kv_status) | systemctl is-active=$(sys_active)"
expect_stopped "stop后" || true

# ---------------------------------------------------------------------------
# 9. uninstall -> 单元移除
# ---------------------------------------------------------------------------
log "== 9. uninstall 服务 =="
if out=$("$BIN" -config "$CFG" uninstall 2>&1); then
  pass "uninstall 成功"
else
  fail "uninstall" "$out"
fi
sleep 2
if systemctl list-unit-files 2>/dev/null | grep -q "^${UNIT_NAME}\\.service"; then
  fail "uninstall" "单元文件仍存在"
else
  pass "uninstall 后单元已移除"
fi
if [ "$(kv_status 2>/dev/null)" = "unknown" ]; then pass "uninstall 后 yigate status=unknown"; else log "    [uninstall 后] yigate status=$(kv_status)"; fi

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
log "=========================================="
log "测试结果: 通过 $PASS  失败 $FAIL"
log "=========================================="
[ "$FAIL" -eq 0 ]
