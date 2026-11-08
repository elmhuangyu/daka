# Daka

A habit tracker with a Go backend and a Vue frontend.

## Layout

- `backend/cmd/daka` - program entry point
- `backend/e2e` - end-to-end tests over the real HTTP stack (`just e2e`)
- `backend/lib/{app,config,auth,db,dbmodels,holiday,schedule,stats,extcal,notify,ratelimit,secret,api}` - backend packages
- `frontend` - Vue 3 SPA (`just fe-install`, `fe-lint`, `fe-test`, `fe-build`)
- `skills/daka` - Claude skill for the read-only AI API

## Development

Requires Go and [just](https://github.com/casey/just).

- `just test` - run backend tests
- `just e2e` - run the end-to-end tests (real router, temp SQLite DB, injected clock, fake OIDC provider)
- `just lint` - golangci-lint if installed, else `go vet` and a gofmt check
- `just fmt` - format code
- `just run` - run the backend

## Deployment and security

- The server binds to `127.0.0.1:8080` by default and runs in single-user mode (no login). Binding a non-loopback address requires `server.allow_insecure_public: true` and an `auth.shared_secret` of at least 16 characters, or OIDC login. Put it behind TLS (and set `DAKA_AUTH_SECURE_COOKIE=1`).
- Behind a reverse proxy, set `server.allowed_hosts` (DNS-rebinding guard) and configure trusted proxies for client IPs. Make the proxy redact the `token=` query parameter from access logs (ICS feed URLs).
- Secrets are read from the environment only and are never stored in the database or config files: `DAKA_SECRET_KEY` (encrypts external calendar URLs; rotate with `DAKA_SECRET_KEY_OLD`) and `DAKA_TELEGRAM_BOT_TOKEN` (enables Telegram reminders, long polling, notify-only).
- API tokens are read-only (`feed:read` for the ICS feed, `read` for the AI API) and sensitive items are never exposed through them.

## Manual verification

Some checks need a real third-party service and cannot be automated.

**Google Calendar ignores VALARM (DESIGN C1, F1).** The feed emits a `VALARM`
(`ACTION:DISPLAY`, `TRIGGER:PT0S`) on timed events (items with `remind_time`),
which Apple Calendar honours. Google Calendar is expected to show no reminder
for URL-subscribed calendars, which is why Telegram is the primary reminder
channel. `just e2e` only pins the feed structure; to verify the Google side:

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
