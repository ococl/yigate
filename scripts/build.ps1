#!/usr/bin/env pwsh
# build.ps1 — local cross-compile validation for Windows developers.
# Usage: .\scripts\build.ps1
# Output binaries are written to .\build

param(
    [string]$Version = "dev",
    [string]$OutputDir = "./build"
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$SrcDir = Join-Path $RepoRoot "src"
$BuildDir = Join-Path $RepoRoot (Resolve-Path -Relative $OutputDir).TrimStart('.\')

$Commit = (git -C $RepoRoot rev-parse --short HEAD 2>$null) | Out-String
if (-not $Commit) { $Commit = "unknown" }
$Commit = $Commit.Trim()

$BuildTime = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ").ToString()

$LDFLAGS = "-s -w -X main.version=$Version -X main.commit=$Commit -X main.buildTime=$BuildTime"

$Platforms = @(
    "linux/amd64",
    "linux/arm64",
    "linux/386",
    "linux/arm/v7",
    "linux/riscv64",
    "linux/ppc64le",
    "linux/s390x",
    "linux/mips64",
    "linux/mips64le",
    "darwin/amd64",
    "darwin/arm64",
    "windows/amd64",
    "windows/386",
    "windows/arm64",
    "freebsd/amd64",
    "freebsd/386",
    "freebsd/arm64",
    "openbsd/amd64",
    "openbsd/arm64",
    "netbsd/amd64"
)

if (-not (Test-Path $BuildDir)) {
    New-Item -ItemType Directory -Path $BuildDir -Force | Out-Null
}

$Env:GOPROXY = "https://goproxy.cn,direct"
$Env:CGO_ENABLED = "0"

foreach ($Platform in $Platforms) {
    $Parts = $Platform -split '/'
    $Env:GOOS = $Parts[0]
    $Env:GOARCH = $Parts[1]
    $Env:GOARM = if ($Parts[2]) { $Parts[2].Substring(1) } else { "" }

    $OutputName = "yigate-$Env:GOOS-$Env:GOARCH"
    if ($Env:GOARCH -eq "arm" -and $Env:GOARM -eq "7") {
        $OutputName = "yigate-$Env:GOOS-armv7"
    }
    if ($Env:GOOS -eq "windows") {
        $OutputName += ".exe"
    }

    $OutputPath = Join-Path $BuildDir $OutputName
    Write-Host "Building $OutputName ..." -ForegroundColor Cyan

    Push-Location $SrcDir
    try {
        go build -trimpath -ldflags $LDFLAGS -o $OutputPath .
        if ($LASTEXITCODE -ne 0) {
            throw "Build failed for $Platform"
        }
    }
    finally {
        Pop-Location
    }
}

Write-Host "All binaries written to $BuildDir" -ForegroundColor Green
