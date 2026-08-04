BINARY      = servlo
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
BUILD_DIR   = ./build
INSTALL_DIR = $(HOME)/.local/bin
UI_DIR      = internal/ui/web

# JS runtime for the web UI build. Prefer npm (when servlo manages Node), fall
# back to bun so servlo can be built on a host where Node is unmanaged. Checks
# ~/.bun/bin too since the bun installer may leave it off a non-login PATH.
# Both run the same package.json scripts; only the install verb differs
# (npm ci vs bun install).
JS_PM := $(shell command -v npm 2>/dev/null || command -v bun 2>/dev/null || echo $(HOME)/.bun/bin/bun)
ifeq ($(notdir $(JS_PM)),bun)
JS_INSTALL = install
else
JS_INSTALL = ci
endif

PKG        = github.com/realrashid/servlo/internal/version
LDFLAGS    = -s -w \
             -X $(PKG).Version=$(VERSION) \
             -X $(PKG).Commit=$(COMMIT) \
             -X $(PKG).Date=$(DATE)

.PHONY: build build-server build-server-all build-ui install-ui-deps test-ui install install-installer test clean release release-snapshot

# Architectures a Servlo droplet can be. build-server targets one at a time so
# a release job can fan out; build-server-all is the local "does it still cross
# build" check.
SERVER_ARCHES = amd64 arm64
SERVER_ARCH  ?= amd64

UI_INSTALL_STAMP = $(UI_DIR)/node_modules/.package-lock.json

install-ui-deps: $(UI_INSTALL_STAMP)

$(UI_INSTALL_STAMP): $(UI_DIR)/package-lock.json $(UI_DIR)/package.json
	cd $(UI_DIR) && $(JS_PM) $(JS_INSTALL)
	@touch $@

build-ui: $(UI_INSTALL_STAMP)
	cd $(UI_DIR) && $(JS_PM) run build

test-ui: $(UI_INSTALL_STAMP)
	cd $(UI_DIR) && $(JS_PM) run test

build: build-ui
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/servlo

# build-server is the production target. CGO_ENABLED=0 so the binary is static
# and runs on any Ubuntu 24.04 droplet regardless of its glibc, GOOS=linux
# because that is the only platform Servlo supports, and -trimpath so the
# shipped binary carries no build-host paths.
build-server: build-ui
	CGO_ENABLED=0 GOOS=linux GOARCH=$(SERVER_ARCH) go build -trimpath \
		-ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-$(SERVER_ARCH) ./cmd/servlo

build-server-all: build-ui
	@for arch in $(SERVER_ARCHES); do \
		echo "building linux/$$arch"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath \
			-ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-$$arch ./cmd/servlo || exit 1; \
	done

install: build
	install -Dm755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed $(INSTALL_DIR)/$(BINARY)"
	@if [ "$$(uname)" = "Darwin" ]; then \
		launchctl kickstart -k gui/$$(id -u)/com.servlo.servlo-panel 2>/dev/null && echo "Restarted servlo-panel" || true; \
		launchctl kickstart -k gui/$$(id -u)/com.servlo.servlo-watcher 2>/dev/null && echo "Restarted servlo-watcher" || true; \
	else \
		systemctl --user daemon-reload 2>/dev/null || true; \
		systemctl --user is-active --quiet servlo-panel 2>/dev/null && systemctl --user restart servlo-panel && echo "Restarted servlo-panel" || true; \
		systemctl --user is-active --quiet servlo-watcher 2>/dev/null && systemctl --user restart servlo-watcher && echo "Restarted servlo-watcher" || true; \
	fi

# Install the installer script as 'servlo-installer' so users can run
# servlo-installer --update  or  servlo-installer --uninstall
install-installer:
	install -Dm755 install.sh $(INSTALL_DIR)/servlo-installer
	@echo "Installed $(INSTALL_DIR)/servlo-installer"

test:
	go test ./...

test-installer:
	bats tests/installer/installer.bats

test-all: test test-ui test-installer

clean:
	rm -rf $(BUILD_DIR)
	rm -rf $(UI_DIR)/dist

# Requires goreleaser: https://goreleaser.com/install/
release:
	goreleaser release --clean

release-snapshot:
	goreleaser release --snapshot --clean
