# AGENTS.md

Guidance for AI coding agents working on Daka, a habit tracker (Go backend, Vue 3 frontend).

## Conventions

- English only: code, comments, docs, UI strings, commit messages.
- `tmp/` is gitignored scratch space for planning notes. Never `git add -f` it.
- Do not push, open PRs, or use real Telegram/OIDC credentials without the user's consent.
- Linear history only: no merge commits. Rebase the task branch onto `main`, then `git merge --ff-only` (the repo sets `merge.ff=only` and `pull.rebase=true`). Remove task worktrees and branches once merged.
- Keep dependencies light. Do not add heavy libraries without a design reason.

## Commands

Run from the repo root (needs Go, [just](https://github.com/casey/just), pnpm):

| Command | Purpose |
| --- | --- |
| `just fmt` | goimports (backend) + oxfmt (frontend) |
| `just lint` | golangci-lint + oxlint/vue-tsc |
| `just build` | Backend binary into `build/`, frontend into `frontend/dist` |
| `just test` | `go test -race ./...` (includes `backend/e2e`) + Vitest |
| `just build-image` | Docker image |
| `just update-deps` | Update Go and pnpm dependencies |
| `just run` / `run-full` | Run the backend / build the frontend and serve it |
| `just fe-install` / `fe-dev` | Install frontend deps / dev server (proxies `/api`) |

Before finishing a change, run `just fmt lint build test`.

## Layout

- `backend/cmd/daka`: entry point, config, server wiring, `user claim` subcommand.
- `backend/lib/`: `app` (handler assembly, bootstrap), `api` (HTTP handlers, OpenAPI), `auth` (sessions, OIDC, CSRF, reauth, HostGuard), `config`, `db` (SQLite/Postgres), `dbmodels` (GORM models, `registry.go`), `schedule` (RRULE, holidays, quota), `holiday`, `stats`, `extcal`, `notify` (Telegram), `ratelimit`, `secret`.
- `backend/e2e`: cross-cutting scenario tests over the real HTTP stack.
- `frontend/src`: Vue 3 + Vite + TS + Tailwind v4 + DaisyUI + Pinia. `views/`, `components/`, `stores/`, `offline/` (IndexedDB queue), `i18n/`; PWA files in `frontend/public`.
- `skills/daka/SKILL.md`: Claude skill for the read-only AI API.

## Domain model (do not break)

- `Item` + immutable `ItemRuleVersion` (effective from a date; ties go to the highest id) + `CheckinLog`. Changing a rule only ever inserts a new version; never edit or delete history.
- Dates are local `YYYY-MM-DD` computed with `UserSettings.timezone`. Timestamps are stored in UTC.
- There are no DB foreign keys. Cascades live in the service layer. A new user-owned or item-owned table must be added to `dbmodels/registry.go` so export, import, and account deletion cover it.
- Each item has a single quota. Schedules use RRULE (rrule-go) plus `holiday_mode`.

## Privacy and security rules

- Sensitive items (item flag OR category flag) must never appear in logs, error messages, Telegram messages, the ICS feed, or AI/read-token responses. Error strings must not include user data such as item names.
- `?redact=1` is applied server-side. The frontend privacy guard (Ctrl+Shift+H) clears plaintext synchronously and discards stale responses.
- Secrets (`DAKA_SECRET_KEY`, `DAKA_SECRET_KEY_OLD`) come from the environment only. Never store them in the DB, config files, logs, or the repo. User-configured secrets (such as per-user Telegram bot tokens and external calendar URLs) are encrypted at rest with `DAKA_SECRET_KEY`.
- API tokens are read-only and hashed (SHA-256). `dk_` Bearer tokens go through `tokenGate` (GET allowlist in `readAllowed`); the feed token works only on `/api/calendar/feed.ics`. A new GET route is not token-accessible unless added to the allowlist on purpose.
- Dangerous routes (delete item/category/account, tokens, export, import, Telegram link) sit behind `Reauth`. Mutating requests require the `X-Daka-CSRF` header.
- Outbound fetches (external calendars, holiday data) must stay SSRF-safe.
- Telegram is per-user and notify-only: each user can configure their own bot token to receive a daily morning digest plus optional per-item `remind_time`. No inline check-in.

## Testing expectations

- Backend: table-driven tests next to the code; use the injected clock (`Deps.Now`), never `time.Now` in logic under test. Privacy-relevant changes need a leak test (see `backend/e2e`).
- Frontend: Vitest with jsdom. Icons are pre-registered in `icons.ts` (no runtime fetches), so add new icons there.
- Service worker, IndexedDB, and PWA behaviour are not verified in a real browser by the test suite; check them manually when changing them.

## Known open items

- Whether Google Calendar lets users choose a colour for a subscribed feed, and whether it ignores the feed's `VALARM`, needs manual verification (README, "Manual verification").
- HostGuard is off in login mode unless `server.allowed_hosts` is set, and there is no startup warning.
