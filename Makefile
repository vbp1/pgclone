# Makefile for pgclone (Go port)

export PATH := $(HOME)/go/bin:$(PATH)

.PHONY: build test lint docker-test

build:
	go build ./...

test:
	go test -v ./...

GOLANGCI_LINT_VERSION ?= v1.57.2

TOOLS_BIN := $(GOPATH)/bin

install-tools:
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(TOOLS_BIN) $(GOLANGCI_LINT_VERSION)
	@echo "Installing gofumpt..."
	@go install mvdan.cc/gofumpt@latest

lint:
	@which golangci-lint >/dev/null || { echo "golangci-lint not found. Run 'make install-tools' first."; exit 1; }
	@test "$$(golangci-lint --version | grep -Eo 'v[0-9]+\.[0-9]+\.[0-9]+')" = "$(GOLANGCI_LINT_VERSION)" || { echo "golangci-lint version mismatch. Run 'make install-tools'."; exit 1; }
	golangci-lint run

docker-test:
	docker compose -f testdata/docker-compose.yml up --build --abort-on-container-exit 