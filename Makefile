.PHONY: all check build test integration-test run fix fmt-check lint audit cover clean help

## all: check + build
all: check build

## check: CI gate (fmt-check, test, integration-test, lint, audit) — use in CI
check: fmt-check test integration-test lint audit

## build: verify package compilation
build:
	go build ./...

## test: run tests with race detector
test:
	go test -race ./...

## integration-test: run real-engine conformance tests
integration-test:
	cd integration && go test -race ./...

## fix: apply go fmt, go fix, and go mod tidy to both modules
fix:
	go fmt ./...
	cd integration && go fmt ./...
	go fix ./...
	cd integration && go fix ./...
	go mod tidy
	cd integration && go mod tidy

## fmt-check: exit non-zero if any .go files are unformatted
fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

## lint: run go vet and staticcheck
lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...

## audit: run pinned gosec and govulncheck
audit:
	go run github.com/securego/gosec/v2/cmd/gosec@v2.27.1 ./...
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

## cover: generate HTML coverage report → coverage.html
cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

## clean: remove generated coverage files
clean:
	rm -f coverage.out coverage.html

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/^## /  /'
