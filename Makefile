# Makefile for pgclone (Go port)

export PATH := $(HOME)/go/bin:$(PATH)

.PHONY: build test lint docker-test integration-build integration-test

build:
	CGO_ENABLED=0 go build ./...

test:
	go test -v ./...

GOLANGCI_LINT_VERSION ?= latest

TOOLS_BIN := $(GOPATH)/bin

install-tools:
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(TOOLS_BIN) $(GOLANGCI_LINT_VERSION)
	@echo "Installing gofumpt..."
	@go install mvdan.cc/gofumpt@latest

lint:
	@which golangci-lint >/dev/null || { echo "golangci-lint not found. Run 'make install-tools' first."; exit 1; }
	golangci-lint run ./...

docker-test:
	docker compose -f testdata/docker-compose.yml up --build --abort-on-container-exit 

# Build Linux/amd64 binary and place it into integration/docker for Dockerfile COPY
integration-build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o integration/docker/pgclone ./cmd/pgclone

# Run integration tests (requires Docker/Compose running locally)
integration-test: integration-build
	go test -tags=integration ./integration -v 