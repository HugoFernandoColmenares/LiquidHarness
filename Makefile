SHELL := /bin/sh
GO ?= go

# Windows needs the .exe suffix to run the binary from cmd.exe and PowerShell.
ifeq ($(OS),Windows_NT)
EXE := .exe
endif
BINARY := liquidharness$(EXE)
PKG := github.com/HugoFernandoColmenares/LiquidHarness
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X $(PKG)/internal/version.Version=$(VERSION) \
  -X $(PKG)/internal/version.Commit=$(COMMIT) \
  -X $(PKG)/internal/version.Date=$(DATE)
LICENSE_SHA256 := cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30

.DEFAULT_GOAL := help
.PHONY: help build build-embedded test test-race cover lint fmt fmt-check vet vuln bench \
        check run tui eval docs-check license-check install-local clean snapshot tools

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

build: ## Build the standard binary (CGO disabled)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/liquidharness

build-embedded: ## Build with embedded llama.cpp (requires CGO + toolchain)
	CGO_ENABLED=1 $(GO) build -tags embedded -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-embedded ./cmd/liquidharness

test: ## Run unit and integration tests
	$(GO) test ./...

test-race: ## Run tests with the race detector
	$(GO) test -race -shuffle=on ./...

cover: ## Produce a coverage profile and summary
	$(GO) test -coverprofile=coverage.txt -covermode=atomic ./...
	$(GO) tool cover -func=coverage.txt | tail -n 1

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code
	gofumpt -w .
	goimports -w .

fmt-check: ## Fail if code is not formatted
	@test -z "$$(gofumpt -l .)" || { gofumpt -l .; echo "run: make fmt"; exit 1; }
	@test -z "$$(goimports -l .)" || { goimports -l .; echo "run: make fmt"; exit 1; }

check: fmt-check vet lint test ## Run all local checks

vet: ## Run go vet
	$(GO) vet ./...

vuln: ## Check known vulnerabilities
	govulncheck ./...

bench: ## Run benchmarks
	$(GO) test -run '^$$' -bench . -benchmem ./...

run: build ## Run the CLI
	./bin/$(BINARY) $(ARGS)

tui: build ## Open the TUI
	./bin/$(BINARY)

eval: build ## Run the smoke eval set
	./bin/$(BINARY) eval run --dataset smoke

docs-check: ## Validate links and code fences in documentation
	$(GO) run ./scripts/dev/checkdocs

license-check: ## Verify LICENSE matches the canonical Apache-2.0 text
	@actual=$$(sha256sum LICENSE | cut -d' ' -f1); \
	if [ "$$actual" != "$(LICENSE_SHA256)" ]; then \
		echo "LICENSE hash mismatch: $$actual"; exit 1; \
	fi

install-local: build ## Install to GOPATH/bin
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/liquidharness

clean: ## Remove build artifacts
	rm -rf bin dist coverage.txt coverage.html

snapshot: ## Build a local GoReleaser snapshot
	goreleaser release --snapshot --clean

tools: ## Install development tools
	$(GO) install mvdan.cc/gofumpt@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	$(GO) install github.com/goreleaser/goreleaser/v2@latest
