#!/usr/bin/env bash
set -euo pipefail

# sync-github.sh — 把 CNB 主仓的代码 / 标签 / Release / 附件同步到 GitHub 镜像仓。
#
# 设计前提：
#   * CNB 是唯一的构建与发布平台，GitHub 仅作镜像分发，不再运行任何 CI。
#   * 本脚本不依赖 GitHub Actions，只用 git + curl 直连 GitHub REST API。
#
# 用法:
#   scripts/sync-github.sh [code|release|meta|all]
#     code     推送 main 分支与全部标签
#     release  创建/更新 Release 并上传 release/ 目录下的产物与 SBOM
#     meta     更新仓库简介（description）与 topics
#     all      依次执行以上全部（默认）
#
# 环境变量:
#   GITHUB_TOKEN  必填。fine-grained PAT 需具备：
#                   Contents: Read and write         （推送代码/标签、读写 Release 与附件）
#                   Administration: Read and write   （仅 meta 需要，改 description/topics）
#   GITHUB_REPO   仓库全名，默认 ococl/yigate
#   GITHUB_USER   推送用的用户名，默认取仓库 owner
#   GITHUB_BRANCH 同步的分支，默认 main
#   GITHUB_API    API 地址，默认 https://api.github.com
#   DRY_RUN       非空时只打印将执行的动作，不真正写入
#
# 幂等性：重复执行不会产生重复 Release 或重复附件（同名附件先删后传）。

# 用 BASH_SOURCE 而非 $0：被 source 时（SOURCE_ONLY=1）仍能定位仓库根目录。
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
API="${GITHUB_API:-https://api.github.com}"
SLUG="${GITHUB_REPO:-ococl/yigate}"
OWNER="${SLUG%%/*}"
BRANCH="${GITHUB_BRANCH:-main}"
USER_NAME="${GITHUB_USER:-$OWNER}"
DRY_RUN="${DRY_RUN:-}"

# 仓库元数据：与 README、VERSIONING.md 保持一致。
REPO_DESCRIPTION="轻量级统一代理网关：HTTP / HTTPS CONNECT / SOCKS5 单端口接入，支持规则路由、多上游、健康检查、socks5h 远端解析、配置热加载与内置管理界面。"
REPO_HOMEPAGE=""
REPO_TOPICS=(
  proxy
  gateway
  socks5
  http-proxy
  https-proxy
  reverse-proxy
  golang
  self-hosted
  webui
  single-port
  rule-routing
  health-check
)

log()  { printf '[sync-github] %s\n' "$*"; }
warn() { printf '[sync-github] WARN: %s\n' "$*" >&2; }
die()  { printf '[sync-github] ERROR: %s\n' "$*" >&2; exit 1; }

require_token() {
  [[ -n "${GITHUB_TOKEN:-}" ]] || die "缺少 GITHUB_TOKEN。请在 CNB 仓库设置中配置该环境变量（或通过密钥仓库 imports 注入），并确保 token 具备 Contents: Read and write 权限。"
}

# api <method> <path> [curl args...]
# 输出响应体，失败时把 HTTP 状态与 GitHub 的错误信息一起打印出来。
api() {
  local method="$1" path="$2"; shift 2
  local url="${API}${path}"
  local body_file status
  body_file="$(mktemp)"

  status="$(curl -sS -o "$body_file" -w '%{http_code}' -X "$method" \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "$@" "$url" 2>/dev/null || true)"

  if [[ ! "$status" =~ ^2[0-9][0-9]$ ]]; then
    local msg
    msg="$(head -c 800 "$body_file")"
    rm -f "$body_file"
    die "GitHub API ${method} ${path} 返回 ${status:-无响应}: ${msg}"
  fi

  cat "$body_file"
  rm -f "$body_file"
}

# 上传二进制附件：uploads.github.com 不接受 JSON 头，需与 REST API 分开处理。
# 单个归档上传失败即中断发布，因此对网络抖动做有限重试。
upload_asset() {
  local release_id="$1" file="$2"
  local name url status attempt max=3
  name="$(basename "$file")"
  url="https://uploads.github.com/repos/${SLUG}/releases/${release_id}/assets?name=${name}"

  if [[ -n "$DRY_RUN" ]]; then
    log "DRY_RUN 上传附件 ${name}"
    return 0
  fi

  for attempt in $(seq 1 "$max"); do
    status="$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      -H "Content-Type: application/octet-stream" \
      --data-binary "@${file}" "$url" || true)"
    if [[ "$status" =~ ^2[0-9][0-9]$ ]]; then
      log "已上传附件 ${name}"
      return 0
    fi
    warn "上传附件 ${name} 失败（HTTP ${status:-无响应}，第 ${attempt}/${max} 次）"
    sleep 2
  done
  die "上传附件 ${name} 连续 ${max} 次失败"
}

# ---------------------------------------------------------------- 代码与标签
# CNB 构建环境可能是浅克隆，向 GitHub 推送需要完整对象与标签；
# 不影响只读操作（DRY_RUN 不触发）。
ensure_full_history() {
  local shallow
  shallow="$(git -C "$REPO_ROOT" rev-parse --is-shallow-repository 2>/dev/null || echo false)"
  [[ "$shallow" == "true" ]] || return 0

  log "检测到浅克隆，补全 Git 历史与标签"
  git -C "$REPO_ROOT" fetch --quiet --unshallow --tags origin \
    || git -C "$REPO_ROOT" fetch --quiet --deepen=200000 origin \
    || warn "补全历史失败，推送可能被 GitHub 拒绝"
}

sync_code() {
  local push_url="https://${USER_NAME}:${GITHUB_TOKEN}@github.com/${SLUG}.git"
  # 禁止凭据协商交互：CNB 构建容器可能带有指向本仓的 credential helper。
  export GIT_TERMINAL_PROMPT=0
  ensure_full_history

  # tag 流水线以 detached HEAD 检出，且只 fetch 了 tag ref，本地可能没有 refs/heads/$BRANCH，
  # 直接推送分支引用会报 "src refspec refs/heads/main does not match any"。
  # 按当前 HEAD 补一个本地分支：tag 检出时 HEAD 就是该 Release 的提交。
  local ref="refs/heads/${BRANCH}"
  if ! git -C "$REPO_ROOT" rev-parse --verify --quiet "$ref" >/dev/null; then
    log "本地缺少分支 ${BRANCH}，按当前 HEAD 创建"
    git -C "$REPO_ROOT" branch -f "${BRANCH}" HEAD
  fi

  log "推送代码 ${BRANCH} -> ${SLUG}"

  if [[ -n "$DRY_RUN" ]]; then
    log "DRY_RUN git push ${SLUG} ${BRANCH}"
    git -C "$REPO_ROOT" -c credential.helper= push --dry-run --quiet "$push_url" "refs/heads/${BRANCH}:refs/heads/${BRANCH}"
  else
    git -C "$REPO_ROOT" -c credential.helper= push --quiet "$push_url" "refs/heads/${BRANCH}:refs/heads/${BRANCH}"
  fi

  local tags
  tags="$(git -C "$REPO_ROOT" tag --list 'v*' | sort -V)"
  if [[ -z "$tags" ]]; then
    warn "本地没有 v* 标签，跳过标签同步"
    return 0
  fi

  log "推送标签：$(echo "$tags" | tr '\n' ' ')"
  if [[ -n "$DRY_RUN" ]]; then
    log "DRY_RUN git push --tags"
  else
    # 只推送不存在的标签，已存在的标签由发布流程保证不可变。
    local tag
    for tag in $tags; do
      if git -C "$REPO_ROOT" ls-remote --exit-code --tags "$push_url" "refs/tags/${tag}" >/dev/null 2>&1; then
        continue
      fi
      git -C "$REPO_ROOT" -c credential.helper= push --quiet "$push_url" "refs/tags/${tag}:refs/tags/${tag}"
      log "已推送标签 ${tag}"
    done
  fi
}

# ------------------------------------------------------------------- Release
resolve_version() {
  if [[ -n "${BUILD_VERSION:-}" ]]; then
    echo "${BUILD_VERSION#v}"
    return 0
  fi
  if [[ -f "${RELEASE_DIR}/.version" ]]; then
    tr -d '[:space:]' < "${RELEASE_DIR}/.version"
    return 0
  fi
  die "无法确定版本号：release/.version 不存在且未提供 BUILD_VERSION"
}

release_body() {
  local tag="$1"
  if [[ -f "${REPO_ROOT}/LATEST_CHANGELOG.md" ]]; then
    cat "${REPO_ROOT}/LATEST_CHANGELOG.md"
    return 0
  fi
  local prev
  prev="$(git -C "$REPO_ROOT" describe --tags --abbrev=0 "${tag}^" 2>/dev/null || true)"
  if [[ -n "$prev" ]]; then
    git -C "$REPO_ROOT" log --no-merges --pretty='- %s (%h)' "${prev}..${tag}"
  else
    git -C "$REPO_ROOT" log --no-merges --max-count=60 --pretty='- %s (%h)' "${tag}"
  fi
}

json_escape() {
  # 把 stdin 文本转成合法的 JSON 字符串字面量（含引号）。
  # 构建镜像不一定带 python3，用 perl 兜底，两者都没有时直接失败而不是产出坏 JSON。
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json,sys;sys.stdout.write(json.dumps(sys.stdin.read()))'
  elif command -v perl >/dev/null 2>&1; then
    perl -0777 -pe 's/([\\"])/\\$1/g; s/\n/\\n/g; s/\r/\\r/g; s/\t/\\t/g; s/^/"/; s/$/"/'
  else
    die "需要 python3 或 perl 来生成 JSON 请求体"
  fi
}

# release_payload <tag> <version> <body_file> — 生成创建 Release 的 JSON 请求体。
release_payload() {
  local tag="$1" version="$2" body_file="$3" prerelease="false"
  # 带预发布后缀（X.Y.Z-<prerelease>）的版本在 GitHub 侧同样标记为 prerelease，
  # 避免它被当成默认下载的稳定版。
  if [[ "$version" == *-* ]]; then
    prerelease="true"
  fi
  printf '{"tag_name":%s,"name":%s,"draft":false,"prerelease":%s,"body":' \
    "$(printf '%s' "$tag" | json_escape)" \
    "$(printf 'YiGate %s' "$version" | json_escape)" \
    "$prerelease"
  json_escape < "$body_file"
  printf '}'
}

# asset_id_for <release_id> <name> — 在 Release 附件列表中查同名附件的 id。
asset_id_for() {
  local release_id="$1" name="$2"
  api GET "/repos/${SLUG}/releases/${release_id}/assets?per_page=100" 2>/dev/null \
    | tr -d '\n' | sed 's/},{/}\n{/g' \
    | grep -F "\"name\":\"${name}\"" \
    | sed -n 's/^{"url":"[^"]*","id":\([0-9]*\).*/\1/p' | head -1
}

sync_release() {
  local version tag release_id existing
  version="$(resolve_version)"
  tag="v${version}"
  log "同步 Release ${tag}"

  [[ -d "$RELEASE_DIR" ]] || die "release/ 目录不存在，无法同步附件"

  release_id="$(api GET "/repos/${SLUG}/releases/tags/${tag}" 2>/dev/null \
    | sed -n 's/^[[:space:]]*"id": \([0-9]\+\),*$/\1/p' | head -1 || true)"

  local body_file
  body_file="$(mktemp)"
  release_body "$tag" > "$body_file"

  if [[ -z "$release_id" ]]; then
    if [[ -n "$DRY_RUN" ]]; then
      log "DRY_RUN 创建 Release ${tag}"
      release_id="0"
    else
      local payload
      payload="$(mktemp)"
      release_payload "$tag" "$version" "$body_file" > "$payload"
      release_id="$(api POST "/repos/${SLUG}/releases" \
        -H 'Content-Type: application/json' --data-binary "@${payload}" \
        | sed -n 's/^[[:space:]]*"id": \([0-9]\+\),*$/\1/p' | head -1)"
      rm -f "$payload"
      log "已创建 Release ${tag}（id=${release_id}）"
    fi
  else
    log "Release ${tag} 已存在（id=${release_id}），复用"
    if [[ -z "$DRY_RUN" ]]; then
      local payload
      payload="$(mktemp)"
      {
        printf '{"body":'
        json_escape < "$body_file"
        printf '}'
      } > "$payload"
      api PATCH "/repos/${SLUG}/releases/${release_id}" \
        -H 'Content-Type: application/json' --data-binary "@${payload}" >/dev/null
      rm -f "$payload"
      log "已更新 Release 说明"
    fi
  fi
  rm -f "$body_file"

  # 附件：先删除同名旧附件，保证重复执行结果一致。
  local assets
  assets="$(find "$RELEASE_DIR" -maxdepth 1 -type f \
    \( -name '*.tar.gz' -o -name '*.zip' -o -name '*.spdx.json' -o -name 'SHA256SUMS' \) | sort)"
  [[ -n "$assets" ]] || die "release/ 目录下没有可上传的产物"

  # 同名旧附件必须先串行删除，否则并行上传会与删除交错。
  local file name asset_id
  while IFS= read -r file; do
    [[ -n "$file" ]] || continue
    name="$(basename "$file")"
    asset_id="$(asset_id_for "$release_id" "$name" || true)"
    if [[ -n "$asset_id" && -z "$DRY_RUN" ]]; then
      api DELETE "/repos/${SLUG}/releases/assets/${asset_id}" >/dev/null \
        || warn "删除旧附件 ${name} 失败"
      log "已移除同名旧附件 ${name}"
    fi
  done <<< "$assets"

  # 上传阶段并行执行：26 平台约 30 个文件，串行建连开销显著。
  # 并发数保守取 4，任何单个文件仍走 upload_asset 的 3 次重试；
  # 全部结束后统一汇报失败，避免中途 die 留下半截上传状态。
  upload_assets_parallel "$release_id" "$assets"
}

# upload_assets_parallel <release_id> <换行分隔的文件列表>
upload_assets_parallel() {
  local release_id="$1" files="$2"
  local max_jobs="${UPLOAD_JOBS:-4}"
  local running=0 failed=0 file pid
  local pids=()

  while IFS= read -r file; do
    [[ -n "$file" ]] || continue
    upload_asset "$release_id" "$file" &
    pids+=("$!")
    running=$((running + 1))
    if (( running >= max_jobs )); then
      pid="${pids[0]}"
      wait "$pid" || failed=$((failed + 1))
      pids=("${pids[@]:1}")
      running=$((running - 1))
    fi
  done <<< "$files"

  if (( ${#pids[@]} > 0 )); then
    for pid in "${pids[@]}"; do
      wait "$pid" || failed=$((failed + 1))
    done
  fi

  if (( failed > 0 )); then
    die "${failed} 个附件上传失败（并发 ${max_jobs}）"
  fi
}

# -------------------------------------------------------------- 仓库元数据
sync_meta() {
  log "更新仓库简介与 topics"

  local payload
  payload="$(mktemp)"
  printf '{"description":%s' "$(printf '%s' "$REPO_DESCRIPTION" | json_escape)" > "$payload"
  if [[ -n "$REPO_HOMEPAGE" ]]; then
    printf ',"homepage":%s' "$(printf '%s' "$REPO_HOMEPAGE" | json_escape)" >> "$payload"
  fi
  printf '}' >> "$payload"

  if [[ -n "$DRY_RUN" ]]; then
    log "DRY_RUN PATCH /repos/${SLUG}"
  else
    if api PATCH "/repos/${SLUG}" -H 'Content-Type: application/json' \
        --data-binary "@${payload}" >/dev/null 2>&1; then
      log "已更新 description"
    else
      warn "更新 description 失败：token 需要 Administration: Read and write 权限，可在 GitHub 网页手动填写"
    fi
  fi
  rm -f "$payload"

  # topics 均为 ASCII 单词，直接拼 JSON，避免依赖 python3。
  local topics_json sep=""
  topics_json='{"names":['
  local topic
  for topic in "${REPO_TOPICS[@]}"; do
    topics_json+="${sep}\"${topic}\""
    sep=","
  done
  topics_json+=']}'

  if [[ -n "$DRY_RUN" ]]; then
    log "DRY_RUN PUT /repos/${SLUG}/topics"
  else
    local status
    status="$(printf '%s' "$topics_json" | curl -sS -o /dev/null -w '%{http_code}' -X PUT \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      -H "Content-Type: application/json" \
      --data-binary @- "${API}/repos/${SLUG}/topics" 2>/dev/null || true)"
    if [[ "$status" =~ ^2[0-9][0-9]$ ]]; then
      log "已更新 topics（${#REPO_TOPICS[@]} 个）"
    else
      warn "更新 topics 失败（HTTP ${status:-无响应}）：token 需要 Administration: Read and write 权限"
    fi
  fi
}

# ---------------------------------------------------------------------- 入口
main() {
  local action="${1:-all}"
  require_token
  cd "$REPO_ROOT"

  case "$action" in
    code)    sync_code ;;
    release) sync_release ;;
    meta)    sync_meta ;;
    all)     sync_code; sync_release; sync_meta ;;
    *)       die "未知动作 '$action'，可用：code | release | meta | all" ;;
  esac
  log "完成：${action}"
}

# SOURCE_ONLY=1 只加载函数不执行，供测试或其它脚本复用。
if [[ "${SOURCE_ONLY:-}" != "1" ]]; then
  main "$@"
fi
