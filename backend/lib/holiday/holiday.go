// Package holiday provides holiday and workday determination.
package holiday

import (
	"sort"
	"time"
)

// Kind classifies a calendar day.
type Kind string

const (
	// Normal means no override: Mon-Fri work, Sat/Sun rest.
	Normal Kind = "normal"
	// Holiday is a statutory day off.
	Holiday Kind = "holiday"
	// Workday is a makeup working day (normally a weekend).
	Workday Kind = "workday"
)

// RegionCN is the only region with holiday data. Every other region
// (including "US") is unsupported: plain weekend semantics, Published=false.
const RegionCN = "CN"

// DayInfo describes one date.
type DayInfo struct {
	Kind     Kind   `json:"kind"`
	Name     string `json:"name,omitempty"`
	IsOffDay bool   `json:"isOffDay"`
}

// Day is a DayInfo with its date, used by YearInfo.
type Day struct {
	Date     string `json:"date"`
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	IsOffDay bool   `json:"isOffDay"`
}

// YearData is the payload for the future /api/calendar/holidays endpoint.
type YearData struct {
	Year      int   `json:"year"`
	Published bool  `json:"published"`
	Days      []Day `json:"days"`
}

// Calendar answers day questions for one region.
type Calendar struct {
	p      *Provider
	region string
}

// NewCalendar returns a Calendar for region (case-sensitive, "CN" supported).
func NewCalendar(p *Provider, region string) *Calendar {
	return &Calendar{p: p, region: region}
}

func (c *Calendar) data(year int) (map[string]rawDay, bool) {
	if c.region != RegionCN || c.p == nil {
		return nil, false
	}
	return c.p.lookup(year)
}

// Published reports whether holiday data exists for year.
func (c *Calendar) Published(year int) bool {
	_, ok := c.data(year)
	return ok
}

// Day classifies a YYYY-MM-DD date. An unparsable date is Normal.
func (c *Calendar) Day(date string) DayInfo {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		return DayInfo{Kind: Normal}
	}
	if m, ok := c.data(t.Year()); ok {
		if d, ok := m[date]; ok {
			if d.IsOffDay {
				return DayInfo{Kind: Holiday, Name: d.Name, IsOffDay: true}
			}
			return DayInfo{Kind: Workday, Name: d.Name}
		}
	}
	return DayInfo{Kind: Normal, IsOffDay: isWeekend(t)}
}

// IsWorkday reports whether the date is a working day. Invalid dates are not.
func (c *Calendar) IsWorkday(date string) bool {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return false
	}
	return !c.Day(date).IsOffDay
}

// IsStatutoryHoliday reports whether the date is a listed statutory day off.
func (c *Calendar) IsStatutoryHoliday(date string) bool {
	return c.Day(date).Kind == Holiday
}

// YearInfo returns all special days of year, sorted by date.
// Days is never nil; Published is false when no data exists.
func (c *Calendar) YearInfo(year int) YearData {
	out := YearData{Year: year, Days: []Day{}}
	m, ok := c.data(year)
	if !ok {
		return out
	}
	out.Published = true
	for _, d := range m {
		k := Workday
		if d.IsOffDay {
			k = Holiday
		}
		out.Days = append(out.Days, Day{Date: d.Date, Name: d.Name, Kind: k, IsOffDay: d.IsOffDay})
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Date < out.Days[j].Date })
	return out
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}
