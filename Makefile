# Makefile for pgclone (Go port)

.PHONY: build test lint docker-test

build:
	go build ./...

test:
	go test -v ./...

lint:
	golangci-lint run

docker-test:
	docker compose -f testdata/docker-compose.yml up --build --abort-on-container-exit 