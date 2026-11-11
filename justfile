# List all available recipes
default:
    @just --list

module := "github.com/elmhuangyu/daka/backend"

# Local-development only key so per-user Telegram bot tokens can be stored. Never use it for real
# data; set DAKA_SECRET_KEY in the environment to override it.
dev_secret_key := "daka-dev-only-key-not-for-production"

# Format backend (goimports) and frontend (oxfmt)
fmt:
    cd backend && goimports -w -local "{{module}}" .
    cd frontend && pnpm run fmt

# Lint backend (golangci-lint) and frontend (oxlint + vue-tsc)
lint:
    cd backend && golangci-lint run ./...
    cd frontend && pnpm run lint

# Build version: UTC build time and short commit
version := `date -u +%Y-%m-%d-%H-%M-%S` + "-" + `git rev-parse --short=8 HEAD`

# Build the backend binary into build/ and the frontend into frontend/dist
build:
    mkdir -p build && cd backend && go build -trimpath -o ../build/ ./cmd/daka
    cd frontend && pnpm run build

# Test backend (incl. e2e, race detector) and frontend
test:
    cd backend && go test -race ./...
    cd frontend && pnpm run test

# Build the Docker image
build-image:
    docker build -t daka:main -t daka:{{version}} .

# Run go mod tidy
tidy:
    cd backend && go mod tidy

# Update Go module dependencies
update-go-deps:
    cd backend && go get -u -t ./...
    @just tidy

# Update pnpm dependencies
update-pnpm-deps:
    cd frontend && pnpm update

# Update both Go and pnpm dependencies
update-deps: update-go-deps update-pnpm-deps

# Run the backend (127.0.0.1:8080, single-user mode)
run:
    cd backend && DAKA_SECRET_KEY="${DAKA_SECRET_KEY:-{{dev_secret_key}}}" go run ./cmd/daka

# Install frontend dependencies
fe-install:
    cd frontend && pnpm install

# Run the frontend dev server (proxies /api to 127.0.0.1:8080)
fe-dev:
    cd frontend && pnpm dev

# Build the frontend, then run the backend serving it
run-full:
    cd frontend && pnpm run build
    cd backend && DAKA_SECRET_KEY="${DAKA_SECRET_KEY:-{{dev_secret_key}}}" DAKA_STATIC_DIR="{{justfile_directory()}}/frontend/dist" go run ./cmd/daka
