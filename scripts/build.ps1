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
$WebDir = Join-Path $RepoRoot "web"
$WebUIDist = Join-Path $SrcDir "proxy/webui_dist"
$BuildDir = [System.IO.Path]::GetFullPath((Join-Path $RepoRoot $OutputDir))

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
    "linux/arm/v6",
    "linux/riscv64",
    "linux/ppc64le",
    "linux/s390x",
    "linux/mips64",
    "linux/mips64le",
    "linux/mips",
    "linux/mipsle",
    "linux/loong64",
    "darwin/amd64",
    "darwin/arm64",
    "windows/amd64",
    "windows/386",
    "windows/arm64",
    "freebsd/amd64",
    "freebsd/386",
    "freebsd/arm64",
    "freebsd/arm/v7",
    "openbsd/amd64",
    "openbsd/arm64",
    "netbsd/amd64",
    "netbsd/arm64"
)

if (-not (Test-Path $BuildDir)) {
    New-Item -ItemType Directory -Path $BuildDir -Force | Out-Null
}

$Env:GOPROXY = "https://goproxy.cn,direct"
$Env:CGO_ENABLED = "0"

Push-Location $WebDir
try {
    npm ci
    if ($LASTEXITCODE -ne 0) { throw "Frontend dependency installation failed" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
}
finally {
    Pop-Location
}
Remove-Item $WebUIDist -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $WebUIDist -Force | Out-Null
Copy-Item (Join-Path $WebDir "dist/*") $WebUIDist -Recurse -Force
if (-not (Get-ChildItem (Join-Path $WebUIDist "assets") -Filter "*.js" -File -ErrorAction SilentlyContinue)) {
    throw "WebUI build assets are missing"
}

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
