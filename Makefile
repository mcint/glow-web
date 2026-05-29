# glow-web — common dev tasks
#
# Targets here mirror the CLAUDE.md "commit gate": `make check` should pass
# locally before every commit (gofmt + vet + test). `make build` produces a
# binary in ./bin; `make install` puts it on $GOBIN.

BIN_DIR  := bin
BIN      := $(BIN_DIR)/glow-web
PKG      := ./...
CMD      := ./cmd/glow-web

# Single source of the embedded version: `git describe` → injected into the
# version package at link time. Tagged builds report the tag exactly; builds
# between tags report "<tag>-<n>-g<sha>" plus "-dirty" for an unclean tree.
# Plain `go build` / `go run` (no ldflags) fall back to version.Fallback + VCS.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X github.com/mcint/glow-web/internal/version.injected=$(VERSION)

# Default to building. `make` with no target runs the gate first, then build.
.DEFAULT_GOAL := check

.PHONY: help build install run test vet fmt fmt-check check clean screenshots

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary into ./bin (embeds git-describe version)
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(CMD)

install: ## go install into $GOBIN / $GOPATH/bin (embeds git-describe version)
	go install -ldflags "$(LDFLAGS)" $(CMD)

run: build ## Build and serve the project root on 127.0.0.1:8080
	$(BIN) web . --addr 127.0.0.1:8080

test: ## go test ./...
	go test $(PKG)

vet: ## go vet ./...
	go vet $(PKG)

fmt: ## gofmt -w on all Go files
	gofmt -w .

fmt-check: ## Fail if any Go file needs gofmt
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
	  echo "gofmt would change:"; echo "$$out"; exit 1; \
	fi

check: fmt-check vet test ## fmt-check + vet + test — the commit gate

clean: ## Remove build artefacts
	rm -rf $(BIN_DIR)

# Re-capture the README screenshots. Requires playwright + chromium installed
# locally; uses the system chromium so we don't need playwright's bundled one.
# macOS-tested; the chromium path may need tweaking on Linux.
PORT_SHOTS := 18099
CHROMIUM   := /opt/homebrew/bin/chromium
SHOT_DIR   := docs/screenshots

screenshots: build ## Re-capture README screenshots (needs playwright + system chromium)
	@if [ ! -x "$(CHROMIUM)" ]; then \
	  echo "Chromium not at $(CHROMIUM); install or override CHROMIUM=…"; exit 1; \
	fi
	@echo "==> serving project on 127.0.0.1:$(PORT_SHOTS)"
	@$(BIN) web . --addr 127.0.0.1:$(PORT_SHOTS) > /tmp/glow-shots.log 2>&1 & \
	  echo $$! > /tmp/glow-shots.pid; \
	  sleep 0.4
	@CHROMIUM=$(CHROMIUM) PORT=$(PORT_SHOTS) SHOT_DIR=$(SHOT_DIR) \
	  uv run --with playwright python scripts/screenshots.py
	@kill $$(cat /tmp/glow-shots.pid) 2>/dev/null; rm -f /tmp/glow-shots.pid
	@ls -lh $(SHOT_DIR)/
