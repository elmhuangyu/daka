# RRULE library evaluation (P0-5)

Library: `github.com/teambition/rrule-go` **v1.8.2** (pinned in `backend/go.mod`; pure Go, no cgo, MIT).
Tests: `rrule_lib_test.go` (table-driven; expected dates computed by hand from the 2026-2032 calendar).

## Verdict

**Adopt it. All required cases pass; no DESIGN change is required**, only the usage conventions below
(which should be recorded in DESIGN 3.4 / the P0-6 engine).

## Results

| Case | Result |
|---|---|
| `FREQ=MONTHLY;BYDAY=2SU` | OK (Jan 11, Feb 8, Mar 8, Apr 12 ... 2026) |
| `FREQ=MONTHLY;BYDAY=-1FR` | OK (Jan 30, Feb 27, Mar 27, Apr 24 2026) |
| `FREQ=MONTHLY;BYMONTHDAY=-1` | OK, incl. leap Feb 29 2028 |
| `FREQ=DAILY;INTERVAL=3` | OK; phase anchored on dtstart even when the query window starts weeks later |
| `FREQ=MONTHLY;INTERVAL=3;BYDAY=2SU` | OK (Jan, Apr, Jul, Oct); phase is the dtstart month. If dtstart is after that month's 2nd Sunday, that month's hit is not emitted, later ones keep the phase |
| `BYMONTHDAY=31` / `30` / `29` | Short months are skipped (no clamping to month end); Feb 29 only in leap years (2028, 2032) |
| `FREQ=WEEKLY;BYDAY=MO,WE,FR` | OK; dtstart (a Thursday) is not emitted because it does not match |
| Weekly `INTERVAL=2;BYDAY=MO,SU` with `WKST=MO` vs `WKST=SU` | Both correct and different; `WKST` is honoured |
| `FREQ=YEARLY;BYMONTH=10;BYMONTHDAY=1` | OK |
| `UNTIL` | Inclusive; nothing before dtstart is emitted |
| Invalid strings (bad FREQ, bad weekday, `BYMONTHDAY=99`) | Rejected at parse time (map to HTTP 400) |

## Gotchas

1. **DTSTART is mandatory in practice.** If omitted the library silently uses `time.Now()` (with a non-midnight
   time of day), making `INTERVAL>1` non-deterministic. The engine must always set `Dtstart` explicitly from the
   rule version (`TestLib_NoDtstartDefaultsToNow`).
2. **Skipping, not clamping.** `BYMONTHDAY=31` never fires in 30-day months. Users wanting "last day of month" must
   use `BYMONTHDAY=-1`. The UI for "monthly on day N" should offer a "last day" option and warn for N = 29..31.
3. **`Between(after, before, inc)`**: `inc=true` includes both bounds, `inc=false` excludes both. Use `inc=true`
   with date-only bounds. "Is day d due" = `len(Between(d, d, true)) > 0`.
4. **Occurrences carry dtstart's time-of-day and location**, so compare only the `YYYY-MM-DD` part.
5. **Negative ordinals** (`-1FR`, `BYMONTHDAY=-1`) are supported both in strings and via
   `rrule.FR.Nth(-1)` in `ROption`.
6. **Cost.** `Between` on an unbounded rule scans from dtstart; fine for the 30/180-day windows and
   daily checks, but always bound queries (and consider `Until`) for calendar windows far in the future.
7. **UNTIL format**: when DTSTART is UTC, UNTIL must also be UTC (`UNTIL=20260105T000000Z`). Build the string
   from the stored date rather than accepting arbitrary user input.

## Timezone

Daka stores local calendar dates (`YYYY-MM-DD`). Findings:

* A `Dtstart` in a non-UTC location keeps that location in the outputs, so formatting in that location is right,
  but the same instants viewed in UTC are one calendar day earlier (Asia/Shanghai midnight = 16:00Z the day before).
* Mixing zones is the failure mode: a Shanghai-midnight dtstart queried with UTC-midnight window bounds misses
  day one (`TestLib_Timezone`).

**Recommendation (safest):** evaluate everything in UTC with date-only values. Convert the user's local date
`YYYY-MM-DD` to `time.Date(y, m, d, 0,0,0,0, time.UTC)` for dtstart, until, and window bounds; compute "today" in
the user's timezone first, then convert only the resulting calendar date. Read results back with
`t.Format("2006-01-02")` (never `.In(userLoc)`). BYDAY/BYMONTHDAY are then evaluated on the exact stored calendar
dates, independent of DST and of the user's offset.

## Conclusion

No alternative library needed; no DESIGN edit required. Suggested optional DESIGN 3.4 addition: "rrule-go,
evaluated in UTC with date-only values; dtstart always explicit; BYMONTHDAY>28 skips short months (use -1 for last day)".
