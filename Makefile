.PHONY: all build test cross clean release lint web web-build web-embed web-dev web-install web-test web-e2e web-e2e-install

GO ?= go
GOPROXY ?= https://goproxy.cn,direct
CGO_ENABLED ?= 0
GO_BUILD_FLAGS ?= -trimpath -buildvcs=false
SRC_DIR := ./src
WEB_DIR := ./web
BUILD_DIR := ./build
SCRIPTS_DIR := ./scripts

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildTime=$(BUILD_TIME)

all: build

build: web-embed
	cd $(SRC_DIR) && CGO_ENABLED=$(CGO_ENABLED) GOPROXY=$(GOPROXY) $(GO) build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/yigate .

test:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) test ./...

cross: web-embed
	@mkdir -p $(BUILD_DIR)
	@bash -c '\
		source $(SCRIPTS_DIR)/platforms.sh; \
		for platform in $$(all_platforms); do \
			name="$$(output_name "$(VERSION)" "$$platform")"; \
			echo "Building $$name..."; \
			( cd $(SRC_DIR) && env $$(build_env "$$platform") CGO_ENABLED=$(CGO_ENABLED) \
				GOPROXY=$(GOPROXY) $(GO) build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" \
				-o ../$(BUILD_DIR)/$$name . ); \
		done'

clean:
	rm -rf $(BUILD_DIR) release

release:
	@if [ -z "$(V)" ]; then \
		echo "用法: make release V=1.2.0"; \
		exit 1; \
	fi
	./scripts/release.sh $(V)

lint:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) vet ./...

# --- Frontend (web/) ---

web-install:
	cd $(WEB_DIR) && npm ci

web-embed: web-build
	node $(WEB_DIR)/scripts/embed-dist.mjs $(WEB_DIR)/dist $(SRC_DIR)/proxy/webui_dist

web-dev:
	cd $(WEB_DIR) && npm run dev

web-build:
	cd $(WEB_DIR) && npm run build

web-test:
	cd $(WEB_DIR) && npm test

web-e2e-install:
	cd $(WEB_DIR) && npx playwright install --with-deps chromium

web-e2e: web-embed
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && CGO_ENABLED=$(CGO_ENABLED) GOPROXY=$(GOPROXY) go build $(GO_BUILD_FLAGS) -o ../$(BUILD_DIR)/yigate-e2e .
	cd $(WEB_DIR) && npm run test:e2e

web: web-build
