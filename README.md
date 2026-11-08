# Daka

A habit tracker with a Go backend and a Vue frontend.

## Layout

- `backend/cmd/daka` - program entry point
- `backend/e2e` - end-to-end tests over the real HTTP stack (`just e2e`)
- `backend/lib/{config,auth,db,dbmodels,holiday,schedule,notify,api}` - backend packages

## Development

Requires Go and [just](https://github.com/casey/just).

- `just test` - run backend tests
- `just e2e` - run the end-to-end tests (real router, temp SQLite DB, injected clock, fake OIDC provider)
- `just lint` - golangci-lint if installed, else `go vet` and a gofmt check
- `just fmt` - format code
- `just run` - run the backend

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
