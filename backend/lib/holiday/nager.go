package holiday

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// nagerCountries are the countries served from Nager.Date. A region is the
// country code ("US") or country plus subdivision ("CA-ON").
var nagerCountries = map[string]bool{"US": true, "CA": true}

// nagerDay mirrors one entry of the Nager.Date PublicHolidays response.
type nagerDay struct {
	Date     string   `json:"date"`
	Name     string   `json:"name"`
	Global   bool     `json:"global"`
	Counties []string `json:"counties"`
	Types    []string `json:"types"`
}

func parseNager(b []byte, year int) ([]nagerDay, error) {
	var days []nagerDay
	if err := json.Unmarshal(b, &days); err != nil {
		return nil, err
	}
	if len(days) == 0 {
		return nil, errors.New("no days")
	}
	for _, d := range days {
		t, err := time.Parse(dateLayout, d.Date)
		if err != nil {
			return nil, fmt.Errorf("bad date %q", d.Date)
		}
		if t.Year() != year {
			return nil, fmt.Errorf("date %s outside year %d", d.Date, year)
		}
	}
	return days, nil
}

func (p *Provider) setNager(country string, year int, days []nagerDay) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nager[country] == nil {
		p.nager[country] = map[int][]nagerDay{}
	}
	p.nager[country][year] = days
	p.regionCache = map[string]map[int]map[string]rawDay{}
}

// SplitRegion splits "CA-ON" into ("CA", "CA-ON"); "CA" gives ("CA", "").
func SplitRegion(region string) (country, sub string) {
	c, _, ok := strings.Cut(region, "-")
	if !ok {
		return region, ""
	}
	return c, region
}

// lookupRegion returns the public-holiday days of a Nager region and year:
// national ("global") public holidays, plus those of the subdivision if set.
func (p *Provider) lookupRegion(region string, year int) (map[string]rawDay, bool) {
	country, sub := SplitRegion(region)
	if !nagerCountries[country] {
		return nil, false
	}
	p.mu.RLock()
	if m, ok := p.regionCache[region][year]; ok {
		p.mu.RUnlock()
		return m, true
	}
	days, ok := p.nager[country][year]
	p.mu.RUnlock()
	if !ok {
		return nil, false
	}
	out := map[string]rawDay{}
	for _, d := range days {
		if !hasType(d.Types, "Public") {
			continue
		}
		if !d.Global && (sub == "" || !hasType(d.Counties, sub)) {
			continue
		}
		if prev, dup := out[d.Date]; dup {
			if !strings.Contains(prev.Name, d.Name) {
				prev.Name += " / " + d.Name
				out[d.Date] = prev
			}
			continue
		}
		out[d.Date] = rawDay{Name: d.Name, Date: d.Date, IsOffDay: true}
	}
	p.mu.Lock()
	if p.regionCache[region] == nil {
		p.regionCache[region] = map[int]map[string]rawDay{}
	}
	p.regionCache[region][year] = out
	p.mu.Unlock()
	return out, true
}

func hasType(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// RefreshNager fetches the given years for every Nager country. A failing
// country/year keeps its old data and logs a warning.
func (p *Provider) RefreshNager(ctx context.Context, years ...int) {
	for country := range nagerCountries {
		for _, y := range years {
			raw, err := p.expandURL(p.nagerTemplate, y, country)
			var days []nagerDay
			if err == nil {
				var body []byte
				if body, err = p.get(ctx, raw); err == nil {
					days, err = parseNager(body, y)
				}
			}
			switch {
			case err == nil:
				p.setNager(country, y, days)
				log.Info().Str("country", country).Int("year", y).Int("days", len(days)).Msg("holiday data refreshed")
			case errors.Is(err, errNotPublished):
				log.Debug().Str("country", country).Int("year", y).Msg("holiday data not published yet")
			default:
				log.Warn().Err(err).Str("country", country).Int("year", y).Msg("holiday data refresh failed; keeping cached data")
			}
		}
	}
}

// Subdivisions lists the accepted subdivision codes per country.
var Subdivisions = map[string][]string{
	"CA": {"AB", "BC", "MB", "NB", "NL", "NS", "NT", "NU", "ON", "PE", "QC", "SK", "YT"},
	"US": {"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "DC", "FL", "GA", "HI", "ID", "IL", "IN", "IA", "KS", "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ", "NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY"},
}

// ValidRegion reports whether r is a supported holiday region: "CN", a Nager
// country, or a Nager country with a known subdivision ("CA-ON").
func ValidRegion(r string) bool {
	if r == RegionCN {
		return true
	}
	country, sub := SplitRegion(r)
	if !nagerCountries[country] {
		return false
	}
	if sub == "" {
		return true
	}
	for _, s := range Subdivisions[country] {
		if country+"-"+s == sub {
			return true
		}
	}
	return false
}
