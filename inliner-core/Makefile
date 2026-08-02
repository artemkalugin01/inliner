.PHONY: fmt test build run version test-ollama clean

BINARY := inliner-core
BIN_DIR := bin
BIN := $(BIN_DIR)/$(BINARY)

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN) ./cmd/inliner-core

run:
	go run ./cmd/inliner-core stdio

version:
	go run ./cmd/inliner-core version

test-ollama:
	go run ./cmd/inliner-core test-ollama

clean:
	rm -rf $(BIN_DIR)
