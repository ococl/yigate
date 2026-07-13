.PHONY: all build test cross clean release lint

GO ?= go
GOPROXY ?= https://goproxy.cn,direct
SRC_DIR := ./src
BUILD_DIR := ./build

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildTime=$(BUILD_TIME)

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	linux/386 \
	linux/arm/v7 \
	linux/riscv64 \
	linux/ppc64le \
	linux/s390x \
	linux/mips64 \
	linux/mips64le \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/386 \
	windows/arm64 \
	freebsd/amd64 \
	freebsd/386 \
	freebsd/arm64 \
	openbsd/amd64 \
	openbsd/arm64 \
	netbsd/amd64

all: build

build:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) build -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/k5pw .

test:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) test ./...

cross:
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d/ -f1); \
		GOARCH=$$(echo $$platform | cut -d/ -f2); \
		GOARM=$$(echo $$platform | cut -d/ -f3 | sed 's/^v//'); \
		output="k5pw-$${GOOS}-$${GOARCH}"; \
		if [ "$$GOARCH" = "arm" ] && [ "$$GOARM" = "7" ]; then output="k5pw-$${GOOS}-armv7"; fi; \
		if [ "$$GOOS" = "windows" ]; then output="$${output}.exe"; fi; \
		echo "Building $$output..."; \
		cd $(SRC_DIR) && env CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH GOARM=$$GOARM \
			GOPROXY=$(GOPROXY) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/$$output . || exit 1; \
	done

clean:
	rm -rf $(BUILD_DIR)

release:
	@if [ -z "$(V)" ]; then \
		echo "Usage: make release V=1.2.0"; \
		exit 1; \
	fi
	./scripts/release.sh $(V)

lint:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) vet ./...
