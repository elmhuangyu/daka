# List all available recipes
default:
    @just --list

# Run all backend tests
test:
    cd backend && go test ./...

# Lint: golangci-lint if installed, otherwise go vet + gofmt check
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    cd backend
    if command -v golangci-lint >/dev/null 2>&1; then
        golangci-lint run ./...
    else
        echo "golangci-lint not found; falling back to go vet + gofmt"
        go vet ./...
        out="$(gofmt -l .)"
        if [ -n "$out" ]; then
            echo "gofmt needed on:" >&2
            echo "$out" >&2
            exit 1
        fi
    fi

# Format code
fmt:
    cd backend && gofmt -w .

# Run the backend
run:
    cd backend && go run ./cmd/daka

# Install frontend dependencies
fe-install:
    cd frontend && pnpm install

# Run frontend tests
fe-test:
    cd frontend && pnpm test

# Type-check (vue-tsc) and lint (eslint) the frontend
fe-lint:
    cd frontend && pnpm lint

# Build the frontend into frontend/dist
fe-build:
    cd frontend && pnpm build

# Run the frontend dev server (proxies /api to 127.0.0.1:8080)
fe-dev:
    cd frontend && pnpm dev
