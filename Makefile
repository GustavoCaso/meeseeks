.PHONY: build test test-race lint generate-test-coverage clean help

.DEFAULT_GOAL := help

build:
	go build -o meeseeks ./cmd/meeseeks

test:
	go test ./...
	
test-race:
	go test ./... -race

lint:
	golangci-lint run
	
format:
	golangci-lint fmt .

generate-test-coverage:
	@echo "Generating coverage report"
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html
	
clean:
	rm -f meeseeks coverage.out coverage.html

help:
	@echo "Available targets:"
	@echo "  build                  - Build the CLI binary"
	@echo "  test                   - Run all tests"
	@echo "  test-race              - Run all tests with the race detector"
	@echo "  generate-test-coverage - Generate coverage report"
	@echo "  lint                   - Run golangci-lint"
	@echo "  clean                  - Clean build artifacts"
	@echo "  help                   - Show this help message"
