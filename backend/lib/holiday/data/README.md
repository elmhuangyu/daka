# holiday-cn snapshot data

Files `2025.json` and `2026.json` are verbatim copies of the upstream
holiday-cn project (NateScarlet/holiday-cn on GitHub), fetched on 2026-11-07
via the jsDelivr mirror of the master branch
(`https://cdn.jsdelivr.net/gh/NateScarlet/holiday-cn@master/{year}.json`).
They are NOT hand-written or modified. Upstream derives them from the State
Council announcements listed in each file's `papers` field.

SHA-256:

- 2025.json 33e3023d3af27e158d31196a3cc5096aa089ecd5cd33280fb0b92cd9c9d180c9
- 2026.json 0dcfd8004351e132ce15e8444de4df123265b3f490f7e2f8346631122cb5b709

Format: `{"year":N,"days":[{"name","date":"YYYY-MM-DD","isOffDay":bool}]}`.
Only deviations from the plain Mon-Fri calendar are listed.

The snapshot is only a fallback for offline start; the daily refresh replaces
it in memory. Add a new `{year}.json` here whenever a new year is published.
