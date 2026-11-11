#!/usr/bin/env bash
# 复现国密/标准 TLS 的握手与吞吐基线。
#
# 用法：
#   ./scripts/bench-tls.sh                # 全部基准，默认 3s
#   ./scripts/bench-tls.sh BenchmarkHandshake 5s
#
# 数字与 CPU 强相关：SM4 是否走硬件指令、AES-NI 是否存在都会影响结果，
# 密评现场请用同一台机器复测，不要直接引用本仓库的参考值。
set -euo pipefail

cd "$(dirname "$0")/../src"

if ! command -v go >/dev/null 2>&1; then
	echo "go toolchain not found in PATH; install Go 1.27+ or add it to PATH" >&2
	exit 1
fi

pattern="${1:-.}"
benchtime="${2:-3s}"

exec go test ./proxy -run '^$' -bench "${pattern}" -benchtime="${benchtime}" -count=1
