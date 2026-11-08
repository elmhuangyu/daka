package schedule

import (
	"errors"
	"testing"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func rv(n int, unit dbmodels.RollingUnit) dbmodels.ItemRuleVersion {
	return dbmodels.ItemRuleVersion{
		ID: 1, EffectiveFrom: "2026-01-01", DtStart: "2026-05-01",
		ScheduleType: dbmodels.ScheduleRolling, RollingInterval: n, RollingUnit: unit,
	}
}

func TestNextDue(t *testing.T) {
	d, m, y := dbmodels.RollingDay, dbmodels.RollingMonth, dbmodels.RollingYear
	tests := []struct {
		name  string
		v     dbmodels.ItemRuleVersion
		dates []string
		want  string
	}{
		{"due 5/1 done 5/20, 3 months", rv(3, m), []string{"2026-05-20"}, "2026-08-20"},
		{"days", rv(10, d), []string{"2026-02-25"}, "2026-03-07"},
		{"day across leap day", rv(1, d), []string{"2028-02-28"}, "2028-02-29"},
		{"month clamp non-leap", rv(1, m), []string{"2026-01-31"}, "2026-02-28"},
		{"month clamp leap", rv(1, m), []string{"2028-01-31"}, "2028-02-29"},
		{"month clamp to 30", rv(1, m), []string{"2026-03-31"}, "2026-04-30"},
		{"month across year", rv(2, m), []string{"2026-11-30"}, "2027-01-30"},
		{"12 months", rv(12, m), []string{"2026-03-15"}, "2027-03-15"},
		{"year clamp leap day", rv(1, y), []string{"2028-02-29"}, "2029-02-28"},
		{"4 years leap day", rv(4, y), []string{"2028-02-29"}, "2032-02-29"},
		{"unsorted dups use latest", rv(1, d), []string{"2026-05-02", "2026-05-09", "2026-05-02"}, "2026-05-10"},
		{"never completed uses dtstart + interval", rv(3, m), nil, "2026-08-01"},
		{"never completed empty slice", rv(3, m), []string{}, "2026-08-01"},
	}
	for _, tt := range tests {
		got, err := NextDue(tt.v, tt.dates)
		if err != nil || got != tt.want {
			t.Errorf("%s: NextDue = %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
	// dtstart empty falls back to effective_from.
	v := rv(1, m)
	v.DtStart = ""
	if got, err := NextDue(v, nil); err != nil || got != "2026-02-01" {
		t.Errorf("effective_from fallback = %q, %v", got, err)
	}
}

func TestNextDueRecompute(t *testing.T) {
	v := rv(1, dbmodels.RollingMonth)
	logs := []string{"2026-03-10", "2026-04-12"}
	if got, _ := NextDue(v, logs); got != "2026-05-12" {
		t.Fatalf("base = %s", got)
	}
	// Undo the latest completion: falls back to the previous one.
	if got, _ := NextDue(v, logs[:1]); got != "2026-04-10" {
		t.Errorf("after undo = %s, want 2026-04-10", got)
	}
	// Undo everything: back to the start point.
	if got, _ := NextDue(v, nil); got != "2026-06-01" {
		t.Errorf("after undoing all = %s, want 2026-06-01", got)
	}
	// Backfill earlier than the latest: unchanged.
	if got, _ := NextDue(v, []string{"2026-03-10", "2026-04-12", "2026-01-05"}); got != "2026-05-12" {
		t.Errorf("after older backfill = %s, want 2026-05-12", got)
	}
	// Backfill later than the latest: moves forward.
	if got, _ := NextDue(v, []string{"2026-03-10", "2026-04-12", "2026-04-20"}); got != "2026-05-20" {
		t.Errorf("after newer backfill = %s, want 2026-05-20", got)
	}
}

func TestNextDueInvalid(t *testing.T) {
	good := rv(1, dbmodels.RollingDay)
	badStart := good
	badStart.DtStart = "bad"
	bad := []struct {
		v     dbmodels.ItemRuleVersion
		dates []string
	}{
		{rv(0, dbmodels.RollingDay), nil},
		{rv(-1, dbmodels.RollingDay), nil},
		{rv(1001, dbmodels.RollingDay), nil},
		{rv(1, "week"), nil},
		{rv(1, ""), nil},
		{good, []string{"2026-5-1"}},
		{good, []string{"nope"}},
		{good, []string{"1899-12-31"}},
		{badStart, nil},
	}
	for i, b := range bad {
		if _, err := NextDue(b.v, b.dates); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d: err = %v, want ErrInvalid", i, err)
		}
	}
	if got, err := NextDue(rv(1000, dbmodels.RollingYear), []string{"2026-01-01"}); err != nil || got != "3026-01-01" {
		t.Errorf("max interval = %q, %v", got, err)
	}
}

func TestMaintenanceStatus(t *testing.T) {
	// nextDue 2026-05-10, warning window 3 days: warning starts 05-07.
	tests := []struct {
		today string
		want  Status
	}{
		{"2026-05-01", Status{StateOK, 9, 0}},
		{"2026-05-06", Status{StateOK, 4, 0}},
		{"2026-05-07", Status{StateWarning, 3, 0}},
		{"2026-05-09", Status{StateWarning, 1, 0}},
		{"2026-05-10", Status{StateWarning, 0, 0}}, // due day is not overdue
		{"2026-05-11", Status{StateOverdue, 0, 1}},
		{"2026-06-10", Status{StateOverdue, 0, 31}},
	}
	for _, tt := range tests {
		got, err := MaintenanceStatus("2026-05-10", tt.today, 3)
		if err != nil || got != tt.want {
			t.Errorf("today %s: %+v, %v; want %+v", tt.today, got, err, tt.want)
		}
	}
	// warning_days 0: only the due day is warning.
	if got, _ := MaintenanceStatus("2026-05-10", "2026-05-09", 0); got.State != StateOK {
		t.Errorf("wd=0 day before = %+v", got)
	}
	if got, _ := MaintenanceStatus("2026-05-10", "2026-05-10", 0); got.State != StateWarning {
		t.Errorf("wd=0 due day = %+v", got)
	}
	for _, c := range [][2]string{{"x", "2026-05-01"}, {"2026-05-10", "x"}} {
		if _, err := MaintenanceStatus(c[0], c[1], 1); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v: err = %v", c, err)
		}
	}
	if _, err := MaintenanceStatus("2026-05-10", "2026-05-01", -1); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative warning days: err = %v", err)
	}
}

func TestValidateRuleRolling(t *testing.T) {
	if err := ValidateRule(rv(3, dbmodels.RollingMonth)); err != nil {
		t.Errorf("good rolling: %v", err)
	}
	for _, v := range []dbmodels.ItemRuleVersion{rv(0, dbmodels.RollingDay), rv(1, "week"), rv(1001, dbmodels.RollingYear)} {
		if err := ValidateRule(v); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: err = %v", v, err)
		}
	}
}
