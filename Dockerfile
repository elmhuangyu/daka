# syntax=docker/dockerfile:1

# Stage 1: build the frontend.
# The pnpm image ships only pnpm; pnpm installs the Node.js runtime itself.
FROM ghcr.io/pnpm/pnpm:12.18.0 AS web
WORKDIR /src/frontend
RUN pnpm add -g node@runtime:26
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# Stage 2: build a static backend binary (pure-Go SQLite driver, no CGO).
FROM golang:1.27-alpine AS server
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/daka ./cmd/daka

# Stage 3: minimal runtime image, non-root.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 daka \
 && mkdir /data && chown daka /data
COPY --from=server /out/daka /app/daka
COPY --from=web /src/frontend/dist /app/web
USER daka
WORKDIR /data
# No secrets are baked in. Provide DAKA_SECRET_KEY, DAKA_AUTH_* etc. at runtime.
# Listening on a non-loopback address requires DAKA_AUTH_ALLOW_INSECURE_PUBLIC=1
# with DAKA_AUTH_SHARED_SECRET, or OIDC.
ENV DAKA_STATIC_DIR=/app/web \
    DAKA_LISTEN=0.0.0.0:8080 \
    DAKA_DB_DSN=/data/daka.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/app/daka"]
