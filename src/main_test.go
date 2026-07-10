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

	dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "src", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"FROM golang:1.21-alpine AS builder",
		"FROM alpine:3.20",
		"go mod download",
		"CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct go build",
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

	compose, err := os.ReadFile(filepath.Join(repoRoot, "dist", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"services:", "k5pw:", "build:", "context: ../src", "dockerfile: Dockerfile", "ports:", `"8891:8891"`, "volumes:", "restart: unless-stopped"} {
		if !bytes.Contains(compose, []byte(want)) {
			t.Fatalf("docker-compose.yml missing %q", want)
		}
	}

	serviceUnit, err := os.ReadFile(filepath.Join(repoRoot, "dist", "k5pw.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Unit]", "After=network.target", "Type=simple", "ExecStart=/usr/local/bin/k5pw -config /etc/k5pw/config.yml", "Restart=on-failure", "RestartSec=5", "User=k5pw", "Group=k5pw", "WantedBy=multi-user.target"} {
		if !bytes.Contains(serviceUnit, []byte(want)) {
			t.Fatalf("k5pw.service missing %q", want)
		}
	}

	release, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"set -euo pipefail", "PLATFORMS=(", "linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64", "freebsd/amd64", "-X main.version", "-X main.commit", "-X main.buildTime", "go build -trimpath", "git -C"} {
		if !bytes.Contains(release, []byte(want)) {
			t.Fatalf("release.sh missing %q", want)
		}
	}
}
