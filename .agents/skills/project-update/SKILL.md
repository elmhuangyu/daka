---
name: project-update
description: >-
  Runbook for updating dependencies, toolchains, Docker base images, and GitHub
  Actions in the daka repository. Use when the user asks to update project
  dependencies, upgrade Go/Node/pnpm/Docker/CI versions, or perform a routine
  dependency audit and bump.
---

# Project Update Runbook

Use this workflow when updating dependencies, runtime toolchains, Docker images,
or GitHub Actions in `daka` (`backend/` and `frontend/`).

Never edit lockfiles (`frontend/pnpm-lock.yaml`, `backend/go.sum`) by hand.

## 1. Go Dependencies (`backend/`)

1. **Check current & outdated dependencies**:
   ```bash
   cd backend && go list -u -m -f '{{if and (not .Indirect) .Update}}{{.Path}}: {{.Version}} -> {{.Update.Version}}{{end}}' all
   ```
2. **Update Go dependencies**:
   - Routine minor/patch/compatible updates:
     ```bash
     just update-go-deps
     ```
     (`just update-go-deps` runs `go get -u -t ./...` in `backend/`, then runs `just tidy`.)
   - For targeted or major version bumps, run `go get <pkg>@<version>` inside `backend/`, then run `just tidy`.
3. **Check for Go vulnerabilities**:
   ```bash
   cd backend && go run golang.org/x/vuln/cmd/govulncheck@latest ./...
   ```
4. **Check Go toolchain version**:
   - Check the `go X.Y` directive in `backend/go.mod`.
   - Keep the Go version aligned with `Dockerfile` (`FROM golang:<ver>-alpine AS server`).
   - `.github/workflows/test-on-push.yml` uses `go-version-file: backend/go.mod`, so CI follows `backend/go.mod` automatically.

## 2. Web Dependencies (`frontend/` with `pnpm`)

Always use `pnpm` (never `npm` or `yarn`).

1. **Check outdated packages**:
   ```bash
   cd frontend && pnpm outdated
   ```
2. **Update dependencies**:
   - Compatible updates within `package.json` ranges:
     ```bash
     just update-pnpm-deps
     ```
   - Major/latest updates (review breaking changes first for `vue`, `vue-router`,
     `pinia`, `vite`, `vitest`, `vue-tsc`, `typescript`, `tailwindcss`,
     `@tailwindcss/vite`, `daisyui`, `oxlint`, `oxfmt`, etc.):
     ```bash
     cd frontend && pnpm update --latest <pkg>
     ```
3. **Audit production dependencies**:
   ```bash
   cd frontend && pnpm audit --prod --audit-level=high
   ```
4. **Check Node & pnpm versions**:
   - `frontend/package.json` (`packageManager`)
   - Keep aligned with `Dockerfile` (`FROM ghcr.io/pnpm/pnpm:<ver>` and `pnpm add -g node@runtime:<ver>`) and `.github/workflows/test-on-push.yml` (`NODE_VERSION`, `PNPM_VERSION`).

## 3. Docker Dependencies & Base Images

Inspect and update `Dockerfile`:

- `FROM ghcr.io/pnpm/pnpm:<ver> AS web` and `RUN pnpm add -g node@runtime:<ver>`
- `FROM golang:<ver>-alpine AS server`
- `FROM alpine:<ver>` (and `apk add --no-cache ca-certificates`)

Checklist:
- Verify the latest stable tags for `ghcr.io/pnpm/pnpm:*`, `golang:*-alpine`, and `alpine:*`.
- Ensure Go, Node, and pnpm versions match across `Dockerfile`, `backend/go.mod`, `frontend/package.json`, and `.github/workflows/test-on-push.yml`.
- Build the image locally to verify:
  ```bash
  just build-image
  ```

## 4. GitHub Actions & CI-Installed Dependencies

Inspect `.github/workflows/test-on-push.yml`, `.github/workflows/push-image.yml`, and `.github/dependabot.yml`.

1. **CI-installed tool & runtime versions** (`.github/workflows/test-on-push.yml` `env:`):
   - `GOIMPORTS_VERSION`
   - `GOLANGCI_LINT_VERSION`
   - `NODE_VERSION`
   - `PNPM_VERSION`
2. **GitHub Action versions**:
   - `.github/workflows/test-on-push.yml`:
     - `actions/checkout`
     - `actions/setup-go`
     - `pnpm/setup`
   - `.github/workflows/push-image.yml`:
     - `actions/checkout`
     - `docker/login-action`
     - `docker/metadata-action`
     - `docker/build-push-action`
   - Check latest action releases with `gh`:
     ```bash
     for repo in \
       actions/checkout \
       actions/setup-go \
       pnpm/setup \
       docker/login-action \
       docker/metadata-action \
       docker/build-push-action
     do
       tag=$(gh api "repos/$repo/releases/latest" --jq '.tag_name')
       echo "$repo: $tag"
     done
     ```

## 5. Verification

Run the full local suite after all updates:

```bash
just fmt lint build test
```

If `Dockerfile` was modified, also run:

```bash
just build-image
```

Check `git status` and `git diff` to verify only expected files changed, and
commit using conventional commits (e.g., `build(deps): ...` or `ci: ...`).
