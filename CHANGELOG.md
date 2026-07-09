# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/lang/zh-CN/).

## [Unreleased]

### Added
- P2-01: `CHANGELOG.md` and release scripts.

## [1.2.0] - 2026-07-09

### Added
- P1-04: `-version` flag prints version, commit and build time injected via `go build -ldflags`.
- P1-08: Centralized error types in `proxy/errors.go` with structured codes for monitoring and logging.
- P1-09: Per-connection and per-tunnel traffic metrics exposed through `k5pw_transferred_bytes_total`.
- P1-06: Optional pprof endpoints on the management port, controlled by `management.pprof_enabled`.
- P1-07: Log compression failures are logged and surfaced instead of silently dropped.
- P1-03: Access control with `allowed_clients` CIDR list, rate limiting and `max_connections` enforcement.
- P1-01: Atomic configuration hot-reload using a new `runtimeContext` snapshot.
- P1-02: Precise SOCKS5 and HTTP error codes mapped to internal error categories (DNS, refused, timeout, protocol).
- P1-05: Core files split into focused units (`upstream_*.go`, `http_*.go`, `socks_*.go`, `router.go`, `logging.go`, `metrics.go`, `management.go`, etc.).

### Changed
- Improved test coverage: `k5pw` package reaches 60.0%, `k5pw/proxy` reaches 71.1%.
- `Server` now tracks active connections and rejects new ones when `max_connections` is exceeded.
- `Tunnel` counts bytes transferred in both directions and reports to the metrics registry.

### Fixed
- P1-07: `dailyRotateWriter` compression errors are now observable.
- Several race conditions and nil-pointer paths discovered during test expansion.

## [1.1.0] - 2026-07-08

### Added
- P0-02: Management port (`management.listen`) providing `/health` JSON endpoint for liveness/readiness probes.
- P0-03: Prometheus-compatible `/metrics` endpoint with at least 10 core metrics (requests, routing decisions, upstream health, transferred bytes, etc.).
- P0-06: Stricter configuration validation: `listen` must be resolvable by `net.Listen`, upstream references in rules must exist, and `max_connections` cannot be negative.
- P0-07: Logger concurrency safety: hot-reload now switches the writer instead of replacing the global logger pointer.
- P0-08: systemd service template (`dist/k5pw.service`) and one-command install script (`dist/install.sh`).
- SOCKS5 BIND and UDP ASSOCIATE support, with upstream cascading and local fallback.
- `socks5h_redirect` mode to prefer domain-based forwarding through SOCKS5 upstreams.
- Auto-routing cache (`auto` action) with `direct_first` and `proxy_first` strategies.
- GEOSITE data loading with lazy code loading and automatic update (`geosite_update.go`).

### Changed
- P0-04: All upstream credential examples use `${ENV_NAME}` style environment variables; no `user:pass@` examples remain in README, AGENTS or `config.sample.yml`.
- P0-05: Dockerfile pinned to `alpine:3.20` and runs as non-root `k5pw` user.

### Fixed
- Potential log loss during configuration hot-reload.
- Missing validation for upstream name references in routing rules.

## [1.0.0] - 2026-07-01

### Added
- Initial public release of k5pw.
- Unified TCP port for HTTP, HTTPS CONNECT and SOCKS5 protocols.
- Multiple upstream support with priority and weighted smooth round-robin selection.
- Domain / suffix / keyword / regex / IP CIDR routing rules.
- HTTP Basic and SOCKS5 username/password authentication.
- Health checks and dead-upstream recovery.
- JSON / console structured logging with rotation.
- Configuration hot-reload via `fsnotify`.

[Unreleased]: https://github.com/example/k5pw/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/example/k5pw/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/example/k5pw/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/example/k5pw/releases/tag/v1.0.0
