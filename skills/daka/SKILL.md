---
name: daka
description: Query a user's Daka check-in data (habits, maintenance tasks, counters) through its read-only HTTP API. Use when the user asks things like "when did I last change the water filter" or "how many times did I drink cola this year".
---

# Daka read-only API

Daka is a self-hosted check-in tracker. This skill lets you answer questions
about the user's data. You can only read: you cannot record check-ins, edit
items or change settings. If the user wants something recorded, tell them to
do it in the Daka web UI.

## Authentication

The user creates a token in the web UI (Settings, Tokens, scope `read`) and
gives you the token (`dk_...`) and the base URL. Send it on every request:

    Authorization: Bearer dk_xxxxxxxx

- Only `GET` requests work. Any other method, or any endpoint not listed
  below, answers `401`.
- Tokens expire (at most one year) and can be revoked. `401` means the token is
  wrong, expired or revoked; ask the user for a new one. Do not retry in a loop:
  repeated failures are locked out with `429` (honour `Retry-After`).
- Never print the token back, store it in files, or send it anywhere else.

The machine-readable spec is at `GET {base}/api/openapi.json` (no token needed).

## Endpoints

- `GET /api/items?q=water filter` finds items by name. Every
  whitespace-separated term must appear in the name (case-insensitive). Add
  `include=stats` to get the stats of every match in one call.
- `GET /api/items/{id}` is one item with its rule and options.
- `GET /api/items/{id}/stats` returns the last time (`last_at`,
  `days_since_last`), counts per period, quota remaining and a one-line
  `summary`. Start here for "when was the last time" questions.
- `GET /api/checkins?item_id=&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=` lists logs
  newest first; follow `next_cursor` via `cursor=` for more pages. Use it to
  count occurrences in a range.
- `GET /api/view/today` shows what is due today and what is done.
- `GET /api/view/calendar?start=&end=` shows per-day status (max 93 days).
- `GET /api/calendar/holidays?year=` lists statutory holidays and make-up
  workdays.

Dates are in the user's own timezone (returned as `timezone` in the views).

## Typical recipes

- Last time: search `items?q=`, take the `id`, read `/api/items/{id}/stats`,
  report `summary` and `last_at`.
- Count in a range: `checkins?item_id=&from=&to=` and count the rows (page
  through `next_cursor`), or use the `counts` in the stats response.
- Remaining quota: `quota` in the stats response.

## Privacy rules

- Items marked sensitive (directly or through their category) are excluded
  from every response to a read token. They do not appear in lists, today or
  calendar views, or check-in logs, and `GET /api/items/{id}` answers `404`
  for them. You cannot unlock them with query parameters.
- A `404` or an empty result therefore may mean "private". Say you could not
  find it; do not speculate about hidden items.
- Report only what the user asked for. Do not copy notes or item names into
  places the user did not ask for.
