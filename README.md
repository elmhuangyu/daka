# Daka

A check-in tracker with a Go backend and a Vue frontend.

## Layout

- `backend/cmd/daka` - program entry point
- `backend/e2e` - end-to-end tests over the real HTTP stack
- `backend/lib/{app,config,auth,db,dbmodels,holiday,schedule,stats,extcal,notify,ratelimit,secret,api}` - backend packages
- `frontend` - Vue 3 SPA (`just fe-install`, `fe-dev`)
- `skills/daka` - Claude skill for the read-only AI API

## Development

Requires Go and [just](https://github.com/casey/just).

- `just fmt` - goimports (backend) and oxfmt (frontend)
- `just lint` - golangci-lint and oxlint/vue-tsc
- `just build` - backend binary into `build/`, frontend into `frontend/dist`
- `just test` - backend tests (including the e2e tests over the real router, temp SQLite DB, injected clock, fake OIDC provider) and Vitest
- `just build-image` - build the Docker image
- `just update-deps` - update Go and pnpm dependencies
- `just run` - run the backend (API only)
- `just run-full` - build the frontend, then run the backend serving it at http://127.0.0.1:8080

## Running with the frontend

The backend serves the built frontend from a plain directory (no `go:embed`). Set `DAKA_STATIC_DIR` (or `server.static_dir`) to a directory containing `index.html`; relative paths are resolved at startup, and a missing directory is a startup error (a directory without `index.html` only logs a warning). `/api/*` always wins over static files, unknown non-file paths fall back to `index.html` (SPA routing), and hashed `/assets/*` are served immutable while `index.html`, `sw.js` and the manifest are always revalidated.

- Local: `just run-full` runs `pnpm run build` and then starts the backend with `DAKA_STATIC_DIR` pointing at `frontend/dist`.
- Docker: the root `Dockerfile` builds the frontend and a static backend binary (no CGO) and runs as a non-root user. The image sets `DAKA_STATIC_DIR=/app/web`, `DAKA_LISTEN=0.0.0.0:8080` and `DAKA_DB_DSN=/data/daka.db`; `/data` is a volume. No secrets are baked in.

  ```sh
  docker build -t daka .
  docker run -d -p 8080:8080 -v daka-data:/data \
    -e DAKA_AUTH_ALLOW_INSECURE_PUBLIC=1 -e DAKA_AUTH_SHARED_SECRET=<16+ chars> \
    -e DAKA_SECRET_KEY=<key> daka
  ```

  Because the container listens on a non-loopback address, it refuses to start without either OIDC (`DAKA_AUTH_ISSUER` and friends) or `DAKA_AUTH_ALLOW_INSECURE_PUBLIC=1` plus `DAKA_AUTH_SHARED_SECRET`. Session cookies are `Secure` by default, so login only works over HTTPS (or on localhost in most browsers): terminate TLS in a reverse proxy in front of the container. For trusted plain-HTTP testing only, add `-e DAKA_AUTH_SECURE_COOKIE=0`. A bind-mounted `/data` must be writable by uid 10001.
- Compose: `example/` holds a `docker-compose.yml`, a fully commented `config.example.yaml` and an `.env.example`. Copy the latter two to `.env` and `config.yaml` (both gitignored), edit, then run `docker compose up -d` from `example/` (pulls `ghcr.io/elmhuangyu/daka:latest`).
- Caveat (known open item): in login mode the HostGuard (DNS-rebinding defence) is off unless `server.allowed_hosts` / `DAKA_ALLOWED_HOSTS` is set, and there is no startup warning. Set it when exposing the container.

## Deployment and security

- The server binds to `127.0.0.1:8080` by default and runs in single-user mode (no login). Binding a non-loopback address requires `server.allow_insecure_public: true` and an `auth.shared_secret` of at least 16 characters, or OIDC login. Put it behind TLS (and set `DAKA_AUTH_SECURE_COOKIE=1`).
- Behind a reverse proxy, set `server.allowed_hosts` (DNS-rebinding guard) and configure trusted proxies for client IPs. Make the proxy redact the `token=` query parameter from access logs (ICS feed URLs).
- Secrets are read from the environment only and are never stored in the database or config files: `DAKA_SECRET_KEY` (encrypts per-user Telegram bot tokens and external calendar URLs; rotate with `DAKA_SECRET_KEY_OLD`). Telegram bots are configured per-user in Settings.
- API tokens are read-only (`feed:read` for the ICS feed, `read` for the AI API) and sensitive items are never exposed through them.

## Manual verification

Some checks need a real third-party service and cannot be automated.

**Google Calendar ignores VALARM (DESIGN C1, F1).** The feed emits a `VALARM`
(`ACTION:DISPLAY`, `TRIGGER:PT0S`) on timed events (items with `remind_time`),
which Apple Calendar honours. Google Calendar is expected to show no reminder
for URL-subscribed calendars, which is why Telegram is the primary reminder
channel. the e2e tests only pin the feed structure; to verify the Google side:

1. Create an item with a `remind_time` a few minutes ahead and a feed token
   (`POST /api/tokens`, scope `feed:read`).
2. In Google Calendar choose Other calendars, From URL, and paste
   `https://<host>/api/calendar/feed.ics?token=<token>`.
3. Wait for the calendar to sync (can take hours), open the event, and confirm
   it shows no notification/reminder, and that no notification fires at the
   event time on a phone with Google Calendar.
4. Optionally subscribe the same URL in Apple Calendar and confirm the alert
   does fire there.
5. Record the outcome (date, Google Calendar client versions) in the project
   notes; if Google does show reminders, revisit DESIGN 6.1/7.5.
