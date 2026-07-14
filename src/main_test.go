package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommandOutputsBuildMetadata(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "k5pw-test")
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
	for _, want := range []string{"k5pw version 9.8.7", "commit testcommit", "built 2026-07-10T00:00:00Z"} {
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
		"FROM golang:1.21-alpine AS builder",
		"FROM alpine:3.20",
		"go mod download",
		"CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct",
		"adduser -D -u 1000 k5pw",
		"COPY --from=builder /build/k5pw /usr/local/bin/k5pw",
		"COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh",
		"USER k5pw",
		"EXPOSE 8891",
		"ENTRYPOINT",
	} {
		if !bytes.Contains(dockerfile, []byte(want)) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}

	// Compose 模板已拆分至 deploy/docker/
	for _, composeFile := range []string{"compose.local.yml", "compose.lan.yml"} {
		compose, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "docker", composeFile))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"services:", "k5pw:", "build:", "context: ../..", "dockerfile: deploy/docker/Dockerfile", "volumes:", "restart: unless-stopped", "healthcheck:"} {
			if !bytes.Contains(compose, []byte(want)) {
				t.Fatalf("%s missing %q", composeFile, want)
			}
		}
	}

	// systemd unit 已迁移至 deploy/systemd/
	serviceUnit, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "systemd", "k5pw.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Unit]", "After=network.target", "Type=simple", "ExecStart=/usr/local/bin/k5pw -config /var/lib/k5pw/config/config.yml", "Restart=always", "RestartSec=10", "StartLimitBurst=64", "SuccessExitStatus=1 2 8 SIGKILL", "User=k5pw", "Group=k5pw", "WantedBy=multi-user.target"} {
		if !bytes.Contains(serviceUnit, []byte(want)) {
			t.Fatalf("k5pw.service missing %q", want)
		}
	}

	// release.sh 已重写，使用 platforms.sh 作为唯一平台清单
	release, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"set -euo pipefail", "source", "platforms.sh", "go build -trimpath", "-X main.version", "-X main.commit", "-X main.buildTime", "git -C"} {
		if !bytes.Contains(release, []byte(want)) {
			t.Fatalf("release.sh missing %q", want)
		}
	}

	// platforms.sh 是唯一平台清单
	platforms, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "platforms.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"K5PW_PLATFORMS=(", "linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64", "freebsd/amd64"} {
		if !bytes.Contains(platforms, []byte(want)) {
			t.Fatalf("platforms.sh missing %q", want)
		}
	}
}
