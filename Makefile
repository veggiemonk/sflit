BINARY  := sflit
MODULE  := github.com/veggiemonk/sflit
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: check build install test lint vet fix clean help

check: lint test build ## Run lint, test, build

build: ## Build the binary
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY) .

install: ## Install to $GOPATH/bin
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' .

test: ## Run tests
	go test -race -count=1 ./...

test-v: ## Run tests (verbose)
	go test -race -count=1 -v ./...

cover: ## Run tests with coverage report
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@rm -f coverage.out

lint: vet ## Run all linters
	golangci-lint run

vet: ## Run go vet
	go vet ./...

fix: ## Run go fix
	go fix ./...
	golangci-lint run --fix

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
