package schedule

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func TestValidateRuleHardening(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		dtstart string
		ok      bool
	}{
		{"daily", "FREQ=DAILY", "2026-11-07", true},
		{"every 3 days", "FREQ=DAILY;INTERVAL=3", "2026-11-07", true},
		{"weekly byday", "FREQ=WEEKLY;BYDAY=MO,WE", "2026-11-07", true},
		{"monthly last day", "FREQ=MONTHLY;BYMONTHDAY=-1", "2026-11-07", true},
		{"yearly", "FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=14", "2026-11-07", true},
		{"count", "FREQ=DAILY;COUNT=5", "2026-11-07", true},
		{"embedded until ok", "FREQ=DAILY;UNTIL=20261231", "2026-11-07", true},
		{"nth weekday of every 3rd month", "FREQ=MONTHLY;INTERVAL=3;BYDAY=2SU", "2026-11-07", true},
		{"last friday monthly", "FREQ=MONTHLY;INTERVAL=1;BYDAY=-1FR", "2026-11-07", true},
		{"last day of month", "FREQ=MONTHLY;BYMONTHDAY=-1", "2026-11-07", true},
		{"yearly oct 1", "FREQ=YEARLY;BYMONTH=10;BYMONTHDAY=1", "2026-11-07", true},
		{"mon wed fri", "FREQ=WEEKLY;BYDAY=MO,WE,FR", "2026-11-07", true},
		{"dtstart at floor", "FREQ=YEARLY", "1900-01-01", true},
		{"dtstart before floor", "FREQ=DAILY", "1000-01-01", false},
		{"hourly", "FREQ=HOURLY", "2026-11-07", false},
		{"minutely", "FREQ=MINUTELY", "2026-11-07", false},
		{"secondly", "FREQ=SECONDLY", "2026-11-07", false},
		{"interval zero", "FREQ=DAILY;INTERVAL=0", "2026-11-07", false},
		{"interval negative", "FREQ=DAILY;INTERVAL=-2", "2026-11-07", false},
		{"interval huge", "FREQ=DAILY;INTERVAL=100000000", "2026-11-07", false},
		{"count zero", "FREQ=DAILY;COUNT=0", "2026-11-07", false},
		{"count negative", "FREQ=DAILY;COUNT=-1", "2026-11-07", false},
		{"until before dtstart", "FREQ=DAILY;UNTIL=20260101", "2026-11-07", false},
		{"until before dtstart datetime", "FREQ=DAILY;UNTIL=20260101T000000Z", "2026-11-07", false},
		{"byhour multiplier", "FREQ=DAILY;BYHOUR=0,1,2,3;BYMINUTE=0,1,2;BYSECOND=0,1", "2026-11-07", false},
		{"embedded dtstart", "DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY", "2026-11-07", false},
		{"too long", "FREQ=DAILY;BYMONTHDAY=" + strings.Repeat("1,", 300) + "1", "2026-11-07", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRule(fv(1, "2026-11-07", tt.dtstart, tt.rule))
			if tt.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestTenYearWindowIsFast(t *testing.T) {
	rules := []string{
		"FREQ=DAILY",
		"FREQ=DAILY;INTERVAL=3",
		"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR,SA,SU",
		"FREQ=MONTHLY;BYMONTHDAY=1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28",
		"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30", // never matches
	}
	for _, r := range rules {
		v := fv(1, "2026-01-01", "2026-01-01", r)
		if err := ValidateRule(v); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		start := time.Now()
		if _, err := DueDates([]dbmodels.ItemRuleVersion{v}, "2026-01-01", "2036-01-01"); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%s: 10-year query took %v", r, d)
		}
	}
}
