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
