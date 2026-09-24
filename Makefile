BINARY   := skeptic
PKG      := github.com/bugyal/skeptic
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the binary
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/skeptic

.PHONY: install
install: ## Install into GOBIN
	go install -ldflags '$(LDFLAGS)' ./cmd/skeptic

.PHONY: test
test: ## Unit tests (fast, no Docker)
	go test ./...

.PHONY: e2e
e2e: ## End-to-end tests against real Docker
	go test -tags e2e -timeout 30m ./e2e/...

.PHONY: lint
lint: ## gofmt, go vet, staticcheck
	@unformatted=$$(gofmt -l . | grep -v '^$$' || true); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed; skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi

.PHONY: fmt
fmt: ## Format all Go source
	gofmt -w .

.PHONY: clean
clean: ## Remove build output and local run evidence
	rm -rf $(BINARY) dist/ .skeptic/

.PHONY: rename
rename: ## Repoint the module path: make rename ORG=your-org
	@test -n "$(ORG)" || (echo "usage: make rename ORG=your-org"; exit 1)
	@old=$$(head -1 go.mod | awk '{print $$2}'); \
	new=github.com/$(ORG)/skeptic; \
	echo "$$old -> $$new"; \
	find . -name '*.go' -o -name '*.json' -o -name '*.md' -o -name 'Makefile' \
		| grep -v '/.git/' \
		| xargs sed -i '' "s|$$old|$$new|g" 2>/dev/null \
		|| find . -name '*.go' -o -name '*.json' -o -name '*.md' -o -name 'Makefile' \
			| grep -v '/.git/' | xargs sed -i "s|$$old|$$new|g"; \
	go mod edit -module "$$new"
	go build ./...

.PHONY: help
help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
