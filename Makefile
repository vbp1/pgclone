.PHONY: all build test lint release install

BINARY_NAME=pgclone
BUILD_DIR=./dist

all: lint test build

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/client
	go build -o $(BUILD_DIR)/$(BINARY_NAME)-agent ./cmd/agent

lint:
	staticcheck ./...

test:
	go test -v ./...

release:
	goreleaser release --clean --skip-publish

install:
	go install ./...

--- goreleaser.yaml ---
project_name: pgclone
builds:
  - main: ./cmd/client/main.go
    binary: pgclone
    goos: [linux]
    goarch: [amd64]
archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md
snapshot:
  name_template: "next"
