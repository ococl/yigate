package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestVersionCommandOutputsBuildMetadata(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yigate-test")
	cmd := exec.Command("go", "build", "-o", bin, "-ldflags", "-X main.version=9.8.7 -X main.commit=testcommit -X main.buildTime=2026-07-10T00:00:00Z", ".")
	cmd.Env = append(os.Environ(), "GOPROXY=https://goproxy.cn,direct")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}

	cmd = exec.Command(bin, "-version")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run version: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"yigate version 9.8.7", "commit testcommit", "built 2026-07-10T00:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output %q missing %q", got, want)
		}
	}
}

func TestDeliveryArtifactsSmoke(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join(".."))

	// Dockerfile 已迁移至 deploy/docker/
	dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"FROM node:",
		"FROM golang:",
		"AS builder",
		"FROM alpine:",
		"go mod download",
		"CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct",
		"embed-dist.mjs",
		"https://cnb.cool/yi.team/yigate",
		"adduser -D -u 1000 yigate",
		"COPY --from=builder /build/yigate /usr/local/bin/yigate",
		"COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh",
		"chown -R yigate:yigate /var/lib/yigate",
		"EXPOSE 8891",
		"ENTRYPOINT",
	} {
		if !bytes.Contains(dockerfile, []byte(want)) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
	// 运行阶段不再使用 USER 指令，由 entrypoint 在 root 启动时降权到 yigate。
	if bytes.Contains(dockerfile, []byte("\nUSER yigate")) {
		t.Fatalf("Dockerfile should not contain a USER directive (use entrypoint privilege drop)")
	}

	// Compose 模板已拆分至 deploy/docker/
	for _, composeFile := range []string{"compose.local.yml", "compose.lan.yml"} {
		compose, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker", composeFile))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"services:", "yigate:", "build:", "context: ../..", "dockerfile: deploy/docker/Dockerfile", "volumes:", "cap_drop:", "cap_add:", "CHOWN", "SETGID", "SETUID", "restart: unless-stopped", "healthcheck:", "setpriv --reuid=1000", "Authorization: Bearer"} {
			if !bytes.Contains(compose, []byte(want)) {
				t.Fatalf("%s missing %q", composeFile, want)
			}
		}
	}

	rootCompose, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"YIGATE_DEFAULT_LISTEN", "YIGATE_DEFAULT_MANAGEMENT", "CHOWN", "SETGID", "SETUID", "setpriv --reuid=1000", "Authorization: Bearer", "build:", "YIGATE_PROXY_BIND:-127.0.0.1"} {
		if !bytes.Contains(rootCompose, []byte(want)) {
			t.Fatalf("docker-compose.yml missing %q", want)
		}
	}

	// systemd unit 已迁移至 deploy/systemd/
	serviceUnit, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "systemd", "yigate.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Unit]", "After=network.target", "Type=simple", "ExecStart=/usr/local/bin/yigate -config /var/lib/yigate/data/config.yml", "Restart=always", "RestartSec=10", "StartLimitBurst=64", "User=yigate", "Group=yigate", "WantedBy=multi-user.target"} {
		if !bytes.Contains(serviceUnit, []byte(want)) {
			t.Fatalf("yigate.service missing %q", want)
		}
	}

	// release.sh 负责发布前校验与 WebUI 构建，制品统一由 ci-build.sh 生成。
	release, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"set -euo pipefail", "npm ci", "npm run build", "embed-dist.mjs", "WebUI 构建产物缺少 JS assets", "\"${SCRIPTS_DIR}/ci-build.sh\"", "YIGATE_REQUIRE_SBOM=1", "git -C"} {
		if !bytes.Contains(release, []byte(want)) {
			t.Fatalf("release.sh missing %q", want)
		}
	}
	if bytes.Contains(release, []byte("for platform in $(all_platforms)")) {
		t.Fatalf("release.sh must not duplicate platform build loops")
	}

	ciBuild, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "ci-build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"source", "platforms.sh", "YIGATE_VERSION", "YIGATE_COMMIT", "YIGATE_BUILD_TIME", "YIGATE_REQUIRE_SBOM", "go mod verify", "-mod=readonly", "-buildvcs=false", "anchore/syft", "*.js.gz"} {
		if !bytes.Contains(ciBuild, []byte(want)) {
			t.Fatalf("ci-build.sh missing %q", want)
		}
	}

	embedScript, err := os.ReadFile(filepath.Join(repoRoot, "web", "scripts", "embed-dist.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gzipSync", "index.html", ".js.gz"} {
		if !bytes.Contains(embedScript, []byte(want)) {
			t.Fatalf("embed-dist.mjs missing %q", want)
		}
	}

	// platforms.sh 是唯一平台清单
	platforms, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "platforms.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"YIGATE_PLATFORMS=(", "linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64", "freebsd/amd64"} {
		if !bytes.Contains(platforms, []byte(want)) {
			t.Fatalf("platforms.sh missing %q", want)
		}
	}
}

func TestReleaseSecurityContracts(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join(".."))
	read := func(parts ...string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{repoRoot}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	cnb := read(".cnb.yml")
	for _, removed := range []string{"early-bird-preview", "dev-latest", "早鸟预览版"} {
		if bytes.Contains(cnb, []byte(removed)) {
			t.Fatalf(".cnb.yml still contains removed preview pipeline marker %q", removed)
		}
	}
	for _, want := range []string{"YIGATE_REQUIRE_SBOM", "check-release-configs.sh", "allowFailure: false"} {
		if !bytes.Contains(cnb, []byte(want)) {
			t.Fatalf(".cnb.yml missing %q", want)
		}
	}
	// 流水线级 env 设有 CGO_ENABLED=0，而 `go test -race` 依赖 cgo，
	// 未在 race 阶段显式覆盖会让流水线在 race 阶段直接失败。
	for i, line := range strings.Split(string(cnb), "\n") {
		if !strings.Contains(line, "go test -race") {
			continue
		}
		if !strings.Contains(line, "CGO_ENABLED=1") {
			t.Fatalf(".cnb.yml:%d race stage must set CGO_ENABLED=1: %s", i+1, strings.TrimSpace(line))
		}
	}
	formalStart := bytes.Index(cnb, []byte("\"v*\":\n  tag_push:"))
	if formalStart < 0 {
		t.Fatal(".cnb.yml missing formal release pipeline")
	}
	formal := cnb[formalStart:]
	// cnbcool/attachments 只能把附件挂到“已存在”的 Release 上，
	// 必须先 create-release 再 upload-artifacts，否则上传阶段 404。
	create := bytes.Index(formal, []byte("name: create-release"))
	upload := bytes.Index(formal, []byte("name: upload-artifacts"))
	if create < 0 || upload < 0 || create > upload {
		t.Fatal("formal release must create the release before uploading attachments")
	}
	if !bytes.Contains(formal[upload:], []byte("allowFailure: false")) {
		t.Fatal("formal release artifact upload must fail closed")
	}
	// main 是分支事件，没有对应 Release；指定 tag: main 会让上传阶段 404，
	// 不填 tag 时插件按 $CNB_COMMIT 挂到 commit 附件。
	betaEnd := bytes.Index(cnb, []byte("\"(main|dev)\":"))
	if betaEnd < 0 {
		t.Fatal(".cnb.yml missing pull_request pipeline")
	}
	beta := cnb[:betaEnd]
	if bytes.Contains(beta, []byte("tag: main")) {
		t.Fatal("main branch has no release; beta attachments must not target tag main")
	}
	if bytes.Contains(beta, []byte("clear-old-artifacts")) {
		t.Fatal("commit attachments need no cleanup stage")
	}

	configGate := read("scripts", "check-release-configs.sh")
	for _, want := range []string{"deploy/config", "deploy/docker", "local-admin-token", "config.lan.yml"} {
		if !bytes.Contains(configGate, []byte(want)) {
			t.Fatalf("check-release-configs.sh missing %q", want)
		}
	}
	fileGate := read("scripts", "check-release-files.sh")
	for _, want := range []string{"spdx.json", "sha256sum -c"} {
		if !bytes.Contains(fileGate, []byte(want)) {
			t.Fatalf("check-release-files.sh missing %q", want)
		}
	}
	// syft 1.x 移除了 `syft dir <path>` 子命令，只接受 `syft scan <source>` 或
	// `dir:<path>` 源表达式；用错形式会让正式发布的 SBOM 生成失败。
	buildScript := read("scripts", "ci-build.sh")
	if !bytes.Contains(buildScript, []byte(`syft "dir:${SRC_DIR}"`)) {
		t.Fatal(`ci-build.sh must invoke syft with the dir:<path> source form`)
	}
	if bytes.Contains(buildScript, []byte(`syft dir "`)) {
		t.Fatal("ci-build.sh still uses the removed `syft dir <path>` form")
	}
	for _, cfg := range [][]string{{"deploy", "config", "config.lan.yml"}, {"deploy", "docker", "config.lan.yml"}} {
		data := read(cfg...)
		if !bytes.Contains(data, []byte("__REPLACE_PROXY_PASSWORD__")) || !bytes.Contains(data, []byte("__REPLACE_MANAGEMENT_TOKEN__")) {
			t.Fatalf("%s must use generated credential placeholders", filepath.Join(cfg...))
		}
		if bytes.Contains(data, []byte("change-me")) || bytes.Contains(data, []byte("local-admin-token")) {
			t.Fatalf("%s still contains legacy fixed credentials", filepath.Join(cfg...))
		}
	}

	quickstart := read("scripts", "quickstart-linux.sh")
	for _, want := range []string{"YIGATE_RELEASE_URL", "SHA256SUMS", "User=ygate"} {
		if !bytes.Contains(quickstart, []byte(want)) {
			t.Fatalf("quickstart-linux.sh missing %q", want)
		}
	}
	if bytes.Contains(quickstart, []byte("GITHUB_REPO")) {
		t.Fatal("quickstart-linux.sh must not depend on the removed GitHub source")
	}
}

// TestWorkflowActionsPinnedToCommitSHA 保证第三方 Action 以 commit SHA 固定，
// 防止 tag 被上游移动导致供应链风险。
func TestWorkflowActionsPinnedToCommitSHA(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join(".."))
	workflows, err := filepath.Glob(filepath.Join(repoRoot, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflow files found")
	}
	usesLine := regexp.MustCompile(`^\s*(?:-\s*)?uses:\s*(\S+)`)
	shaPin := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, wf := range workflows {
		data, err := os.ReadFile(wf)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			m := usesLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ref := m[1]
			if strings.HasPrefix(ref, "./") {
				continue // 本地 Action 无需 pin
			}
			at := strings.LastIndex(ref, "@")
			if at < 0 {
				t.Fatalf("%s: action %q is not pinned", filepath.Base(wf), ref)
			}
			if !shaPin.MatchString(ref[at+1:]) {
				t.Fatalf("%s: action %q must be pinned to a 40-char commit SHA", filepath.Base(wf), ref)
			}
		}
	}
}
