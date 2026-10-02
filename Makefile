# polymarket — a terminal browser for Polymarket market data
#
# Run `make` on its own to list the targets.

BINARY := polymarket
MODULE := github.com/farrellm/polymarket

# Version comes from git, falling back to "dev" outside a checkout. It is
# stamped into internal/cli.version, which `polymarket --version` reports.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X '$(MODULE)/internal/cli.version=$(VERSION)'
GOFLAGS := -trimpath -ldflags "$(LDFLAGS)"

# Where `go install` puts things: GOBIN if set, else GOPATH/bin.
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.DEFAULT_GOAL := help

.PHONY: help
help: ## list the available targets
	@echo "polymarket $(VERSION)"
	@echo
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-zA-Z0-9_-]*:.*## / \
		{printf "  \033[1m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo
	@echo "  install puts the binary in $(GOBIN)"

.PHONY: build
build: ## build ./polymarket
	go build $(GOFLAGS) -o $(BINARY) .

.PHONY: install
install: ## build and install polymarket (see the path below)
	go install $(GOFLAGS) .
	@echo "installed $(BINARY) $(VERSION) -> $(GOBIN)/$(BINARY)"
	@command -v $(BINARY) >/dev/null 2>&1 || \
		echo "note: $(GOBIN) is not on your PATH"

.PHONY: uninstall
uninstall: ## remove the installed polymarket
	rm -f $(GOBIN)/$(BINARY)
	@echo "removed $(GOBIN)/$(BINARY)"

.PHONY: test
test: ## run the tests
	go test ./...

.PHONY: race
race: ## run the tests under the race detector
	go test -race ./...

.PHONY: cover
cover: ## run the tests and open a coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: fmt
fmt: ## format the source
	gofmt -w .

.PHONY: vet
vet: ## run go vet
	go vet ./...

# The linter version CI uses. Pinned so a new release cannot fail the build
# without the pin being updated deliberately.
GOLANGCI_VERSION := v2.13.2
GOLANGCI := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

.PHONY: lint
lint: ## run golangci-lint (see .golangci.yml)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not on PATH; running $(GOLANGCI_VERSION) with go run"; \
		go run $(GOLANGCI) run; \
	fi

.PHONY: tidy
tidy: ## tidy go.mod and go.sum
	go mod tidy

.PHONY: check
check: ## everything CI runs: format check, vet, lint, race tests
	@test -z "$$(gofmt -l .)" || { echo "unformatted files:"; gofmt -l .; exit 1; }
	go vet ./...
	$(MAKE) lint
	go test -race ./...

.PHONY: fixtures
fixtures: ## re-record testdata/*.json from the live API
	go run testdata/record.go

.PHONY: smoke
smoke: ## check the client against the live API (not part of check)
	go test -tags live -run '^TestLive$$' -count=1 -v ./internal/api/...

.PHONY: run
run: build ## build, then start polymarket
	./$(BINARY)

.PHONY: clean
clean: ## remove build output
	rm -f $(BINARY) coverage.out
