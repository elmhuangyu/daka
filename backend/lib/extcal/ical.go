package extcal

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Parser limits.
const (
	// ParseBudget is the wall-clock budget of one Parse call; when it runs out
	// the events parsed so far are returned with Truncated set.
	ParseBudget      = 1500 * time.Millisecond
	maxEventTime     = 50 * time.Millisecond // per recurring event
	maxRRuleEvents   = 300                   // recurring events expanded per Parse
	maxStepsPerEvent = 20000                 // RRULE iterator steps per event
	maxStepsTotal    = 200000                // RRULE iterator steps per Parse call
	maxLineLen       = 64 << 10              // longest logical (unfolded) line kept
)

const maxDuration = 200 * 365 * 24 * time.Hour

const (
	MaxWindow           = 366 * 24 * time.Hour // longest window a Parse call expands
	MaxEvents           = 5000                 // cap on returned events
	maxRawEvents        = 20000                // cap on VEVENT blocks examined
	maxTitleRunes       = 500
	untitledPlaceholder = ""
)

// ErrEmptyWindow is returned when ParseOptions has no usable window.
var ErrEmptyWindow = errors.New("extcal: empty parse window")

// ExtEvent is one external calendar occurrence normalized to the user's
// timezone. For all-day events Start and End are local midnights and End is
// exclusive (the iCalendar convention): a one-day event ends the next day.
type ExtEvent struct {
	Title  string
	Start  time.Time
	End    time.Time
	AllDay bool
}

// ParseOptions selects what Parse returns.
type ParseOptions struct {
	// Loc is the user's timezone: floating times are read in it and results
	// are expressed in it (default UTC).
	Loc *time.Location
	// From (inclusive) and To (exclusive) bound the window; a window longer
	// than MaxWindow is clamped.
	From, To time.Time
	// MaxEvents caps the result (default and maximum MaxEvents).
	MaxEvents int
	// Context bounds the CPU time spent; nil means no deadline.
	Context context.Context
}

// ParseResult is the outcome of Parse.
type ParseResult struct {
	Events []ExtEvent
	// Skipped counts malformed VEVENTs that were ignored.
	Skipped int
	// Truncated reports that MaxEvents was reached.
	Truncated bool
	// Warnings are static descriptions of tolerated problems.
	Warnings []string
}

type prop struct {
	name   string
	params map[string]string
	value  string
}

// Parse extracts the VEVENTs overlapping the window from an iCalendar document.
func Parse(data []byte, o ParseOptions) (ParseResult, error) {
	var res ParseResult
	loc := o.Loc
	if loc == nil {
		loc = time.UTC
	}
	if !o.To.After(o.From) {
		return res, ErrEmptyWindow
	}
	to := o.To
	if to.Sub(o.From) > MaxWindow {
		to = o.From.Add(MaxWindow)
	}
	limit := o.MaxEvents
	if limit <= 0 || limit > MaxEvents {
		limit = MaxEvents
	}
	w := &window{loc: loc, from: o.From, to: to}
	parent := o.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, ParseBudget)
	defer cancel()
	p := &parser{ctx: ctx, w: w, limit: limit, res: &res, overrides: map[string]map[string]bool{}}

	var events [][]prop
	var cur []prop
	inEvent, depth := false, 0
	for _, line := range unfold(data) {
		pr, ok := parseProp(line)
		if !ok {
			continue
		}
		switch {
		case pr.name == "BEGIN":
			if inEvent {
				depth++ // VALARM etc.: its properties are ignored
			} else if strings.EqualFold(pr.value, "VEVENT") {
				inEvent, depth, cur = true, 0, nil
			}
		case pr.name == "END":
			if inEvent && depth > 0 {
				depth--
			} else if inEvent && strings.EqualFold(pr.value, "VEVENT") {
				inEvent = false
				if len(events) >= maxRawEvents {
					res.Skipped++
				} else {
					events = append(events, cur)
				}
			}
		case inEvent && depth == 0:
			cur = append(cur, pr)
		}
	}

	// Pass 1: collect RECURRENCE-ID overrides so master instances can be dropped.
	for _, props := range events {
		if rid := find(props, "RECURRENCE-ID"); rid != nil {
			if t, allDay, err := p.parseTime(*rid); err == nil {
				uid := valueOf(props, "UID")
				if p.overrides[uid] == nil {
					p.overrides[uid] = map[string]bool{}
				}
				p.overrides[uid][instanceKey(t, allDay)] = true
			}
		}
	}
	// Pass 2: expand.
	for _, props := range events {
		if res.Truncated || p.err != nil {
			break
		}
		if p.ctx.Err() != nil {
			p.err = p.ctx.Err()
			break
		}
		p.event(props)
	}
	if p.err != nil {
		if parent.Err() != nil {
			return ParseResult{}, p.err // the caller gave up
		}
		res.Truncated = true // our own wall-clock budget ran out: keep what we have
	}
	sort.SliceStable(res.Events, func(i, j int) bool {
		a, b := res.Events[i], res.Events[j]
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		return a.Title < b.Title
	})
	res.Warnings = dedupe(res.Warnings)
	return res, nil
}

type window struct {
	loc      *time.Location
	from, to time.Time
}

type parser struct {
	ctx         context.Context
	rruleEvents int
	err         error
	steps       int
	w           *window
	limit       int
	res         *ParseResult
	overrides   map[string]map[string]bool
}

func (p *parser) warn(msg string) { p.res.Warnings = append(p.res.Warnings, msg) }

func (p *parser) add(e ExtEvent) {
	if len(p.res.Events) >= p.limit {
		p.res.Truncated = true
		return
	}
	p.res.Events = append(p.res.Events, e)
}

func (p *parser) event(props []prop) {
	if strings.EqualFold(valueOf(props, "STATUS"), "CANCELLED") {
		return
	}
	startProp := find(props, "DTSTART")
	if startProp == nil {
		p.res.Skipped++
		return
	}
	start, allDay, err := p.parseTime(*startProp)
	if err != nil {
		p.res.Skipped++
		return
	}
	end := start
	switch {
	case find(props, "DTEND") != nil:
		e, _, err := p.parseTime(*find(props, "DTEND"))
		if err != nil {
			p.res.Skipped++
			return
		}
		end = e
	case find(props, "DURATION") != nil:
		d, ok := parseDuration(find(props, "DURATION").value)
		if !ok {
			p.res.Skipped++
			return
		}
		end = start.Add(d)
	case allDay:
		end = start.AddDate(0, 0, 1)
	}
	if end.Before(start) {
		end = start
	}
	title := truncateRunes(strings.TrimSpace(unescapeText(valueOf(props, "SUMMARY"))), maxTitleRunes)
	if title == "" {
		title = untitledPlaceholder
	}

	rr := find(props, "RRULE")
	if rr == nil || find(props, "RECURRENCE-ID") != nil {
		p.emit(title, start, end, allDay)
		return
	}
	if p.rruleEvents++; p.rruleEvents > maxRRuleEvents || start.Year() < 1900 {
		p.res.Skipped++
		return
	}
	eventStart := time.Now()
	// Occurrences starting up to one event-length before the window may still
	// overlap it. The lookback is capped: DTEND/DURATION are attacker controlled.
	dur := end.Sub(start)
	look := dur
	if look > MaxWindow {
		look = MaxWindow
	}
	lo := p.w.from.Add(-look - 24*time.Hour)
	hi := p.w.to
	if allDay {
		lo = utcDate(p.w.from.In(p.w.loc)).Add(-look - 24*time.Hour)
		hi = utcDate(p.w.to.In(p.w.loc)).Add(24 * time.Hour)
	}
	ropt, err := rrule.StrToROption(rr.value)
	if err != nil || ropt.Freq >= rrule.HOURLY {
		p.res.Skipped++
		return
	}
	dtstart := start
	if allDay {
		dtstart = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	}
	ropt.Dtstart = dtstart
	// Stop the library's internal scan (which can run to year 9999 when a rule
	// never matches, e.g. BYMONTH=2;BYMONTHDAY=30) at the window end.
	if ropt.Until.IsZero() || ropt.Until.After(hi) {
		ropt.Until = hi
	}
	r, err := rrule.NewRRule(*ropt)
	if err != nil {
		p.res.Skipped++
		return
	}
	exdates := map[string]bool{}
	for _, ex := range props {
		if ex.name != "EXDATE" {
			continue
		}
		for _, v := range strings.Split(ex.value, ",") {
			if t, ad, err := p.parseTime(prop{name: "EXDATE", params: ex.params, value: strings.TrimSpace(v)}); err == nil {
				exdates[instanceKey(t, ad)] = true
			}
		}
	}
	uid := valueOf(props, "UID")

	next := r.Iterator()
	steps := 0
	for {
		occ, ok := next()
		if !ok || !occ.Before(hi) {
			return
		}
		steps++
		p.steps++
		if steps > maxStepsPerEvent || p.steps > maxStepsTotal {
			p.res.Skipped++ // runaway rule: give up on this event
			return
		}
		if steps&0x3f == 0 {
			if p.ctx.Err() != nil {
				p.err = p.ctx.Err()
				return
			}
			if time.Since(eventStart) > maxEventTime {
				p.res.Skipped++
				return
			}
		}
		if occ.Before(lo) {
			continue
		}
		if p.res.Truncated {
			return
		}
		s := occ
		if allDay {
			s = time.Date(occ.Year(), occ.Month(), occ.Day(), 0, 0, 0, 0, p.w.loc)
		}
		key := instanceKey(s, allDay)
		if exdates[key] || p.overrides[uid][key] {
			continue
		}
		e := s.Add(dur)
		if allDay {
			e = s.AddDate(0, 0, int((dur+12*time.Hour)/(24*time.Hour)))
		}
		p.emit(title, s, e, allDay)
	}
}

// emit adds the event when it overlaps the window.
func (p *parser) emit(title string, s, e time.Time, allDay bool) {
	overlaps := s.Before(p.w.to) && e.After(p.w.from)
	if e.Equal(s) { // zero-length: a point in time
		overlaps = !s.Before(p.w.from) && s.Before(p.w.to)
	}
	if !overlaps {
		return
	}
	p.add(ExtEvent{Title: title, Start: s.In(p.w.loc), End: e.In(p.w.loc), AllDay: allDay})
}

func utcDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func instanceKey(t time.Time, allDay bool) string {
	if allDay {
		return "d" + t.Format("20060102")
	}
	return "t" + strconv.FormatInt(t.Unix(), 10)
}

// parseTime reads a DATE or DATE-TIME property value.
func (p *parser) parseTime(pr prop) (time.Time, bool, error) {
	v := strings.TrimSpace(pr.value)
	if strings.EqualFold(pr.params["VALUE"], "DATE") || (len(v) == 8 && !strings.Contains(v, "T")) {
		t, err := time.ParseInLocation("20060102", v, p.w.loc)
		return t, true, err
	}
	utc := strings.HasSuffix(v, "Z") || strings.HasSuffix(v, "z")
	v = strings.TrimRight(v, "Zz")
	var layout string
	switch len(v) {
	case 15:
		layout = "20060102T150405"
	case 13:
		layout = "20060102T1504"
	default:
		return time.Time{}, false, errors.New("bad time")
	}
	loc := p.w.loc
	switch {
	case utc:
		loc = time.UTC
	case pr.params["TZID"] != "":
		l, err := time.LoadLocation(pr.params["TZID"])
		if err != nil {
			p.warn("unknown TZID treated as UTC")
			l = time.UTC
		}
		loc = l
	}
	t, err := time.ParseInLocation(layout, v, loc)
	return t, false, err
}

// parseDuration handles the common iCalendar forms (P1W, P1D, PT1H30M, ...).
func parseDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") || len(s) < 2 {
		return 0, false
	}
	s = s[1:]
	var d time.Duration
	inTime := false
	parts := 0
	num := ""
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			num += string(c)
		case c == 'T':
			inTime = true
		default:
			n, err := strconv.Atoi(num)
			if err != nil || n > 100000 {
				return 0, false
			}
			num = ""
			parts++
			switch {
			case c == 'W' && !inTime:
				d += time.Duration(n) * 7 * 24 * time.Hour
			case c == 'D' && !inTime:
				d += time.Duration(n) * 24 * time.Hour
			case c == 'H' && inTime:
				d += time.Duration(n) * time.Hour
			case c == 'M' && inTime:
				d += time.Duration(n) * time.Minute
			case c == 'S' && inTime:
				d += time.Duration(n) * time.Second
			default:
				return 0, false
			}
			if d > maxDuration {
				return 0, false
			}
		}
	}
	if num != "" || parts == 0 {
		return 0, false
	}
	if neg {
		d = -d
	}
	return d, true
}

// unfold splits data into logical lines (CRLF or LF; continuation lines start
// with a space or tab).
func unfold(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimPrefix(s, "\xef\xbb\xbf")
	var (
		out  []string
		cur  strings.Builder
		have bool
	)
	for _, l := range strings.Split(s, "\n") {
		if len(l) > 0 && (l[0] == ' ' || l[0] == '\t') && have {
			if cur.Len() < maxLineLen {
				cur.WriteString(l[1:])
			}
			continue
		}
		if have {
			out = append(out, cur.String())
			cur.Reset()
			have = false
		}
		if l != "" {
			cur.WriteString(l)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// parseProp splits "NAME;PARAM=v;PARAM2=\"v:2\":value".
func parseProp(line string) (prop, bool) {
	inQuote := false
	colon := -1
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ':':
			if !inQuote {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon <= 0 {
		return prop{}, false
	}
	head, value := line[:colon], line[colon+1:]
	parts := splitUnquoted(head, ';')
	pr := prop{name: strings.ToUpper(strings.TrimSpace(parts[0])), value: value}
	for _, kv := range parts[1:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if pr.params == nil {
			pr.params = map[string]string{}
		}
		pr.params[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(v, `"`)
	}
	return pr, true
}

func splitUnquoted(s string, sep byte) []string {
	var out []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case sep:
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func find(props []prop, name string) *prop {
	for i := range props {
		if props[i].name == name {
			return &props[i]
		}
	}
	return nil
}

func valueOf(props []prop, name string) string {
	if p := find(props, name); p != nil {
		return p.value
	}
	return ""
}

func unescapeText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
