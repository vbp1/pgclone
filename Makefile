.PHONY: all build test lint release install

BINARY_NAME=pgclone
BUILD_DIR=./dist

all: lint test build

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/pgclone

lint:
	staticcheck ./...

test:
	go test -v ./...

release:
	goreleaser release --clean --skip-publish

install:
	go install ./...

