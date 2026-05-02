.PHONY: help build install clean reset test fmt lint lint-fix tidy all dev ci check-clean install-hooks

help:
	@echo "Available commands:"
	@echo "  make build         - Build the lenos binary"
	@echo "  make install       - Install lenos to GOPATH/bin"
	@echo "  make clean         - Remove built binaries"
	@echo "  make reset         - Remove binaries"
	@echo "  make test          - Run tests"
	@echo "  make fmt           - Format code with gofumpt"
	@echo "  make lint          - Run golangci-lint"
	@echo "  make lint-fix     - Fix linting issues"
	@echo "  make tidy          - Tidy go modules"
	@echo "  make all           - Format, tidy, lint, and build"
	@echo "  make ci            - Run all CI checks (lint, test, build)"
	@echo "  make check-clean   - Check if working directory is clean"
	@echo "  make install-hooks - Install lefthook git hooks"

build:
	@echo "Building lenos..."
	@go build -o lenos .
	@echo "✓ Build complete: ./lenos"

install:
	@echo "Installing lenos..."
	@go build -o $(shell go env GOPATH)/bin/lenos .
	@echo "✓ Installed to $(shell go env GOPATH)/bin/lenos"
	@echo "Installing narrate..."
	@go build -o $(shell go env GOPATH)/bin/narrate ./cmd/narrate
	@echo "✓ Installed to $(shell go env GOPATH)/bin/narrate"

clean:
	@echo "Cleaning build artifacts..."
	@rm -f lenos
	@echo "✓ Cleaned build artifacts"

reset: clean
	@echo "✓ Reset complete"

test:
	@echo "Running tests..."
	@go test ./...

tidy:
	@echo "Tidying go modules..."
	@go mod tidy
	@echo "✓ go mod tidy complete"

fmt:
	@echo "Formatting code..."
	@gofumpt -w .
	@echo "✓ Code formatted"

lint:
	@echo "Running golangci-lint..."
	@golangci-lint run ./...

lint-fix:
	@echo "Running golangci-lint with fixes..."
	@golangci-lint run ./... --fix

all: fmt tidy lint build
	@echo "✓ All checks passed and binary built"

dev: all
	@echo "✓ Development build complete"

ci: lint test build
	@echo "✓ CI checks complete"

check-clean:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "❌ Working directory is not clean"; \
		git status --short; \
		exit 1; \
	else \
		echo "✓ Working directory is clean"; \
	fi

install-hooks:
	@lefthook install
	@echo "✓ Lefthook hooks installed (pre-commit: gofumpt + goimports, pre-push: golangci-lint)"
