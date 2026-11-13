// Package stats derives per-item statistics (last occurrence, counts,
// streaks, completion rate, heatmap, quota progress) from check-in logs, and
// caches them safely (see Cache).
package stats

import (
	"fmt"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// Window sizes.
const (
	RecentDays      = 30
	CompletionDays  = 90
	HeatmapDays     = 365
	completionLabel = "completion rate window"
)

// Counts are numbers of log entries (occurrences), not days.
type Counts struct {
	Last30Days int `json:"last_30_days"`
	ThisYear   int `json:"this_year"`
	Total      int `json:"total"`
}

// OptionSummary summarises the logs of one option. OptionID nil groups logs
// without an option (ad-hoc entries, or items without options).
type OptionSummary struct {
	OptionID  *uint   `json:"option_id"`
	Name      string  `json:"name"`
	Unit      string  `json:"unit"`
	Count     int     `json:"count"`
	Sum       float64 `json:"sum"`
	Avg       float64 `json:"avg"`
	LastValue float64 `json:"last_value"`
	LastDate  string  `json:"last_date,omitempty"`
}

// HeatmapDay is one cell of the 365-day heatmap. Level is OptionsMet /
// OptionsTotal for the day (0 with no logs); for negative items every day with
// an occurrence has level 1. Value is the number of logs for boolean,
// maintenance and negative items, the sum for counters and the latest value
// for metrics.
type HeatmapDay struct {
	Date  string  `json:"date"`
	Level float64 `json:"level"`
	Value float64 `json:"value"`
	// OptionsMet and OptionsTotal are the completed / total options for the
	// day (a boolean item has total 1). Both are 0 for negative items and for
	// days without logs.
	OptionsMet   int `json:"options_met"`
	OptionsTotal int `json:"options_total"`
}

// QuotaProgress is the current quota window.
type QuotaProgress struct {
	Comparator  string  `json:"comparator"`
	Window      string  `json:"window"`
	Period      string  `json:"period"`
	WindowStart string  `json:"window_start"`
	WindowEnd   string  `json:"window_end"`
	Used        float64 `json:"used"`
	Limit       float64 `json:"limit"`
	Remaining   float64 `json:"remaining"`
	Ratio       float64 `json:"ratio"`
	Met         bool    `json:"met"`
	Exceeded    bool    `json:"exceeded"`
	// Traffic is green|yellow|red for at_most quotas, empty otherwise.
	Traffic string `json:"traffic,omitempty"`
}

// MaintenanceInfo is the rolling (maintenance style) status.
type MaintenanceInfo struct {
	NextDue      string `json:"next_due"`
	State        string `json:"state"`
	DaysUntilDue int    `json:"days_until_due"`
	OverdueDays  int    `json:"overdue_days"`
}

// StaleInfo is the soft hint of a "none" schedule.
type StaleInfo struct {
	Days  int  `json:"days"`
	Stale bool `json:"stale"`
}

// Stats is the stats payload of one item (DESIGN 11.2). All dates are in the
// user's timezone.
type Stats struct {
	Today    string `json:"today"`
	Timezone string `json:"timezone"`
	// LastAt is the RFC 3339 time of the latest log in the user's timezone.
	LastAt        *string `json:"last_at"`
	LastDate      *string `json:"last_date"`
	DaysSinceLast *int    `json:"days_since_last"`
	Summary       string  `json:"summary"`
	// Counts are log entries (occurrences); ActiveDays are distinct days with logs.
	Counts     Counts          `json:"counts"`
	ActiveDays Counts          `json:"active_days"`
	Options    []OptionSummary `json:"options"`
	Streak     StreakResult    `json:"streak"`
	// CompletionRate90 is completed due days / due days over the last 90 days
	// (frozen days and a still-open today excluded); null when not applicable
	// or there are no due days.
	CompletionRate90 *float64         `json:"completion_rate_90"`
	Heatmap          []HeatmapDay     `json:"heatmap"`
	Quota            *QuotaProgress   `json:"quota"`
	Maintenance      *MaintenanceInfo `json:"maintenance"`
	Stale            *StaleInfo       `json:"stale"`
}

// ComputeStats derives Stats from the input. Pure: no I/O, no clock.
func ComputeStats(in Input) (Stats, error) {
	today, err := time.Parse(dbmodels.DateLayout, in.Today)
	if err != nil {
		return Stats{}, fmt.Errorf("stats: invalid today %q", in.Today)
	}
	loc := time.UTC
	if in.Settings.Timezone != "" {
		if l, lerr := time.LoadLocation(in.Settings.Timezone); lerr == nil {
			loc = l
		}
	}
	st := Stats{Today: in.Today, Timezone: in.Settings.Timezone}
	byDate := logsByDate(in.Logs)

	counts(&st, in, today)
	st.Options = optionSummaries(in)
	if st.Streak, err = Streak(in); err != nil {
		return Stats{}, err
	}
	st.Heatmap = heatmap(in, today, byDate)

	cur := schedule.CurrentVersion(in.Versions, in.Today)
	last := lastLog(in.Logs)
	if last != nil {
		at := last.CheckinTime.In(loc).Format(time.RFC3339)
		st.LastAt, st.LastDate = &at, &last.CheckinDate
		lt, _ := time.Parse(dbmodels.DateLayout, last.CheckinDate)
		d := int(today.Sub(lt).Hours() / 24)
		if d < 0 {
			d = 0
		}
		st.DaysSinceLast = &d
	}
	st.Summary = summary(st)
	if cur == nil {
		return st, nil
	}
	switch cur.ScheduleType {
	case dbmodels.ScheduleFixedRRule:
		if isDailyOngoing(in.Item) {
			if st.CompletionRate90, err = completionRate(in, today, byDate); err != nil {
				return Stats{}, err
			}
		}
	case dbmodels.ScheduleQuota:
		if st.Quota, err = quotaProgress(in, *cur); err != nil {
			return Stats{}, err
		}
	case dbmodels.ScheduleRolling:
		if st.Maintenance, err = maintenance(in, *cur); err != nil {
			return Stats{}, err
		}
	case dbmodels.ScheduleNone:
		s, serr := schedule.StaleStatus(derefDate(st.LastDate), in.Today, cur.StaleAfterDays)
		if serr != nil {
			return Stats{}, serr
		}
		st.Stale = &StaleInfo{Days: s.Days, Stale: s.Stale}
	}
	return st, nil
}

func isDailyOngoing(it dbmodels.Item) bool {
	return it.Kind == dbmodels.KindOngoing && it.Polarity == dbmodels.PolarityPositive && it.TrackerType != dbmodels.TrackerMaintenance
}

func derefDate(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// lastLog is the latest log by (checkin_date, checkin_time, id).
func lastLog(logs []dbmodels.CheckinLog) *dbmodels.CheckinLog {
	var best *dbmodels.CheckinLog
	for i := range logs {
		l := &logs[i]
		if best == nil || l.CheckinDate > best.CheckinDate ||
			(l.CheckinDate == best.CheckinDate && (l.CheckinTime.After(best.CheckinTime) ||
				(l.CheckinTime.Equal(best.CheckinTime) && l.ID > best.ID))) {
			best = l
		}
	}
	return best
}

func summary(st Stats) string {
	if st.LastDate == nil || st.DaysSinceLast == nil {
		return "No records yet"
	}
	switch d := *st.DaysSinceLast; d {
	case 0:
		return fmt.Sprintf("Last: today (%s)", *st.LastDate)
	case 1:
		return fmt.Sprintf("Last: yesterday (%s)", *st.LastDate)
	default:
		return fmt.Sprintf("Last: %d days ago (%s)", d, *st.LastDate)
	}
}

func counts(st *Stats, in Input, today time.Time) {
	from30 := today.AddDate(0, 0, -(RecentDays - 1)).Format(dbmodels.DateLayout)
	yearStart := fmt.Sprintf("%04d-01-01", today.Year())
	days30, daysYear, daysAll := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range in.Logs {
		d := l.CheckinDate
		if d > in.Today {
			continue
		}
		st.Counts.Total++
		daysAll[d] = true
		if d >= from30 {
			st.Counts.Last30Days++
			days30[d] = true
		}
		if d >= yearStart {
			st.Counts.ThisYear++
			daysYear[d] = true
		}
	}
	st.ActiveDays = Counts{Last30Days: len(days30), ThisYear: len(daysYear), Total: len(daysAll)}
}

func optionSummaries(in Input) []OptionSummary {
	type acc struct {
		OptionSummary
		last *dbmodels.CheckinLog
	}
	groups := map[uint]*acc{}
	order := []uint{}
	get := func(id *uint) *acc {
		var k uint
		if id != nil {
			k = *id
		}
		if g, ok := groups[k]; ok {
			return g
		}
		g := &acc{OptionSummary: OptionSummary{OptionID: id, Unit: in.Item.Unit}}
		groups[k] = g
		order = append(order, k)
		return g
	}
	for _, o := range in.Options {
		if o.Archived {
			continue
		}
		id := o.ID
		g := get(&id)
		g.Name, g.Unit = o.Name, o.Unit
	}
	for i := range in.Logs {
		l := &in.Logs[i]
		g := get(l.OptionID)
		if g.Name == "" && l.OptionID != nil {
			for _, o := range in.Options {
				if o.ID == *l.OptionID {
					g.Name, g.Unit = o.Name, o.Unit
				}
			}
		}
		g.Count++
		g.Sum += l.Value
		if g.last == nil || l.CheckinDate > g.last.CheckinDate ||
			(l.CheckinDate == g.last.CheckinDate && (l.CheckinTime.After(g.last.CheckinTime) ||
				(l.CheckinTime.Equal(g.last.CheckinTime) && l.ID > g.last.ID))) {
			g.last = l
		}
	}
	out := make([]OptionSummary, 0, len(order))
	for _, k := range order {
		g := groups[k]
		if g.Count > 0 {
			g.Avg = g.Sum / float64(g.Count)
			g.LastValue, g.LastDate = g.last.Value, g.last.CheckinDate
		}
		out = append(out, g.OptionSummary)
	}
	return out
}

func heatmap(in Input, today time.Time, byDate map[string][]dbmodels.CheckinLog) []HeatmapDay {
	out := make([]HeatmapDay, 0, HeatmapDays)
	for i := HeatmapDays - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i).Format(dbmodels.DateLayout)
		cell := HeatmapDay{Date: d}
		if logs := byDate[d]; len(logs) > 0 {
			cell.Level, cell.Value, cell.OptionsMet, cell.OptionsTotal = dayLevel(in, d, logs)
		}
		out = append(out, cell)
	}
	return out
}

func dayLevel(in Input, date string, logs []dbmodels.CheckinLog) (level, value float64, met, total int) {
	it := in.Item
	if it.Polarity == dbmodels.PolarityNegative {
		return 1, float64(len(logs)), 0, 0
	}
	c := completion(in, date, logs)
	level, met, total = c.Shade(), c.OptionsMet, c.OptionsTotal
	switch it.TrackerType {
	case dbmodels.TrackerCounter:
		for _, l := range logs {
			value += l.Value
		}
	case dbmodels.TrackerMetric:
		last := logs[0]
		for _, l := range logs[1:] {
			if l.CheckinTime.After(last.CheckinTime) || (l.CheckinTime.Equal(last.CheckinTime) && l.ID > last.ID) {
				last = l
			}
		}
		value = last.Value
	default:
		value = float64(len(logs))
	}
	return level, value, met, total
}

func completionRate(in Input, today time.Time, byDate map[string][]dbmodels.CheckinLog) (*float64, error) {
	from := today.AddDate(0, 0, -(CompletionDays - 1)).Format(dbmodels.DateLayout)
	due, err := schedule.DueDatesWith(in.Versions, from, in.Today, in.Calendar)
	if err != nil {
		return nil, nil //nolint:nilerr // mixed history with unsupported spans: no rate
	}
	total, done := 0, 0
	for _, d := range due {
		if completion(in, d, byDate[d]).Done {
			total++
			done++
			continue
		}
		if frozen(in, d) || d == in.Today {
			continue
		}
		total++
	}
	if total == 0 {
		return nil, nil
	}
	r := float64(done) / float64(total)
	return &r, nil
}

func quotaProgress(in Input, cur dbmodels.ItemRuleVersion) (*QuotaProgress, error) {
	cur.QuotaMeasure = schedule.EffectiveMeasure(cur.QuotaMeasure, in.Item.Polarity)
	q, err := schedule.EvaluateQuota(cur, quotaLogs(in.Logs), in.Today, in.Settings.WeekStart)
	if err != nil {
		return nil, err
	}
	window := string(cur.QuotaWindow)
	if window == "" {
		window = string(dbmodels.QuotaWindowCalendar)
	}
	return &QuotaProgress{
		Comparator: string(q.Comparator), Window: window, Period: string(cur.QuotaPeriod),
		WindowStart: q.WindowStart, WindowEnd: q.WindowEnd, Used: q.Used, Limit: q.Limit,
		Remaining: q.Remaining, Ratio: q.Ratio, Met: q.Met, Exceeded: q.Exceeded,
		Traffic: string(schedule.Traffic(q)),
	}, nil
}

func maintenance(in Input, cur dbmodels.ItemRuleVersion) (*MaintenanceInfo, error) {
	dates := make([]string, 0, len(in.Logs))
	for _, l := range in.Logs {
		dates = append(dates, l.CheckinDate)
	}
	next, err := schedule.NextDue(cur, dates)
	if err != nil {
		return nil, err
	}
	s, err := schedule.MaintenanceStatus(next, in.Today, in.Item.WarningDays)
	if err != nil {
		return nil, err
	}
	return &MaintenanceInfo{NextDue: next, State: s.State, DaysUntilDue: s.DaysUntilDue, OverdueDays: s.OverdueDays}, nil
}
