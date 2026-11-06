BINARY  := bin/server
PKG     := github.com/boyulkim/api-template
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)

# Keep these in sync with CI (.github/workflows/ci.yml calls `make lint` and `make vuln`).
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION := v1.9.0

# Load .env into the environment of every recipe, if present.
-include .env
export

.PHONY: help run build test cover lint vuln fmt tidy migration migrate-up migrate-down docker-build docker-up docker-down clean

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

run: ## Run the dev server (dashboard at http://127.0.0.1:8090/_/)
	go run ./cmd/server serve

build: ## Build a static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/server

test: ## Run all tests with the race detector
	go test -race -count=1 ./...

cover: ## Run tests and open the coverage report
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

$(GOLANGCI_LINT):
	mkdir -p bin
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b bin $(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $(GOLANGCI_LINT)

lint: $(GOLANGCI_LINT) ## Run golangci-lint $(GOLANGCI_LINT_VERSION)
	$(GOLANGCI_LINT) run

vuln: ## Fail on symbol-level govulncheck findings ($(GOVULNCHECK_VERSION))
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

fmt: ## Format code
	gofmt -w .

tidy: ## Tidy go.mod
	go mod tidy

migration: ## Create a blank migration: make migration name=add_posts
	go run ./cmd/server migrate create $(name)

migrate-up: ## Apply pending migrations
	go run ./cmd/server migrate up

migrate-down: ## Revert the last migration
	go run ./cmd/server migrate down 1

docker-build: ## Build the production image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t api-template:$(VERSION) .

docker-up: ## Run the production image locally via compose
	docker compose up --build

docker-down: ## Stop compose services
	docker compose down

clean: ## Remove build output and local data
	rm -rf bin coverage.out pb_data
