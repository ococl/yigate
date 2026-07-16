.PHONY: all build test cross clean release lint web web-build web-dev web-install

GO ?= go
GOPROXY ?= https://goproxy.cn,direct
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

build:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) build -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/yigate .

test:
	cd $(SRC_DIR) && GOPROXY=$(GOPROXY) $(GO) test ./...

cross:
	@mkdir -p $(BUILD_DIR)
	@bash -c '\
		source $(SCRIPTS_DIR)/platforms.sh; \
		for platform in $$(all_platforms); do \
			name="$$(output_name "$(VERSION)" "$$platform")"; \
			echo "Building $$name..."; \
			( cd $(SRC_DIR) && env $$(build_env "$$platform") CGO_ENABLED=0 \
				GOPROXY=$(GOPROXY) $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
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
	cd $(WEB_DIR) && npm install

web-dev:
	cd $(WEB_DIR) && npm run dev

web-build:
	cd $(WEB_DIR) && npm run build

web: web-build
