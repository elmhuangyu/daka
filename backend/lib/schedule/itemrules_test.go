package schedule

import (
	"errors"
	"math"
	"testing"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

func baseRule(st dbmodels.ScheduleType, pol dbmodels.Polarity) dbmodels.ItemRuleVersion {
	v := dbmodels.ItemRuleVersion{EffectiveFrom: "2026-11-07", ScheduleType: st, HolidayMode: dbmodels.HolidayNone}
	switch st {
	case dbmodels.ScheduleFixedRRule:
		v.DtStart = "2026-11-07"
		v.RRuleString = "FREQ=DAILY"
	case dbmodels.ScheduleRolling:
		v.RollingInterval = 3
		v.RollingUnit = dbmodels.RollingMonth
	case dbmodels.ScheduleQuota:
		v.QuotaComparator = dbmodels.QuotaAtLeast
		if pol == dbmodels.PolarityNegative {
			v.QuotaComparator = dbmodels.QuotaAtMost
		}
		v.QuotaCount = 3
		v.QuotaPeriod = dbmodels.QuotaWeek
	}
	return v
}

func f64(f float64) *float64 { return &f }
func uptr(n uint) *uint      { return &n }

// ruleWith builds a positive rule of type st and applies mut.
func ruleWith(st dbmodels.ScheduleType, mut func(*dbmodels.ItemRuleVersion)) dbmodels.ItemRuleVersion {
	v := baseRule(st, dbmodels.PolarityPositive)
	if mut != nil {
		mut(&v)
	}
	return v
}

func TestValidateItemRuleMatrix(t *testing.T) {
	trackers := []dbmodels.TrackerType{dbmodels.TrackerBoolean, dbmodels.TrackerCounter, dbmodels.TrackerMaintenance, dbmodels.TrackerMetric}
	schedules := []dbmodels.ScheduleType{dbmodels.ScheduleFixedRRule, dbmodels.ScheduleRolling, dbmodels.ScheduleQuota, dbmodels.ScheduleNone}
	pols := []dbmodels.Polarity{dbmodels.PolarityPositive, dbmodels.PolarityNegative}
	kinds := []dbmodels.Kind{dbmodels.KindOngoing, dbmodels.KindEvent}
	// DESIGN 2 matrix.
	allowed := map[dbmodels.TrackerType]map[dbmodels.ScheduleType]bool{
		dbmodels.TrackerBoolean:     {"fixed_rrule": true, "rolling": true, "quota": true, "none": true},
		dbmodels.TrackerCounter:     {"fixed_rrule": true, "quota": true, "none": true},
		dbmodels.TrackerMaintenance: {"fixed_rrule": true, "rolling": true, "none": true},
		dbmodels.TrackerMetric:      {"fixed_rrule": true, "quota": true, "none": true},
	}
	n := 0
	for _, tr := range trackers {
		for _, st := range schedules {
			for _, pol := range pols {
				for _, k := range kinds {
					n++
					want := allowed[tr][st]
					wantReason := ""
					switch {
					case !want:
						wantReason = ReasonScheduleNotAllowed
					case pol == dbmodels.PolarityNegative && (tr == dbmodels.TrackerCounter || tr == dbmodels.TrackerMaintenance):
						want, wantReason = false, ReasonNegativeTracker
					case pol == dbmodels.PolarityNegative && (st == dbmodels.ScheduleFixedRRule || st == dbmodels.ScheduleRolling):
						want, wantReason = false, ReasonNegativeSchedule
					}
					name := string(tr) + "/" + string(st) + "/" + string(pol) + "/" + string(k)
					t.Run(name, func(t *testing.T) {
						err := ValidateItemRule(ItemSpec{TrackerType: tr, Kind: k, Polarity: pol}, baseRule(st, pol), nil)
						if want {
							if err != nil {
								t.Fatalf("want allowed, got %v", err)
							}
							return
						}
						if !errors.Is(err, ErrInvalid) {
							t.Fatalf("want ErrInvalid, got %v", err)
						}
						if got := Reason(err); got != wantReason {
							t.Fatalf("reason = %q, want %q", got, wantReason)
						}
					})
				}
			}
		}
	}
	if n != 64 {
		t.Fatalf("matrix rows = %d", n)
	}
}

func TestValidateItemRuleEdges(t *testing.T) {
	opt := func(id uint, unit string) OptionSpec { return OptionSpec{ID: id, Unit: unit} }
	two := []OptionSpec{opt(1, "min"), opt(2, "km")}
	sameUnit := []OptionSpec{opt(1, "min"), opt(2, "min")}
	counter := ItemSpec{TrackerType: dbmodels.TrackerCounter}
	metric := ItemSpec{TrackerType: dbmodels.TrackerMetric}
	boolean := ItemSpec{TrackerType: dbmodels.TrackerBoolean}
	maint := ItemSpec{TrackerType: dbmodels.TrackerMaintenance}
	negMetric := ItemSpec{TrackerType: dbmodels.TrackerMetric, Polarity: dbmodels.PolarityNegative}
	negBool := ItemSpec{TrackerType: dbmodels.TrackerBoolean, Polarity: dbmodels.PolarityNegative}
	type mut = func(*dbmodels.ItemRuleVersion)
	quota := func(m mut) dbmodels.ItemRuleVersion { return ruleWith(dbmodels.ScheduleQuota, m) }
	none := func(m mut) dbmodels.ItemRuleVersion { return ruleWith(dbmodels.ScheduleNone, m) }
	rrule := func(m mut) dbmodels.ItemRuleVersion { return ruleWith(dbmodels.ScheduleFixedRRule, m) }
	rolling := func(m mut) dbmodels.ItemRuleVersion { return ruleWith(dbmodels.ScheduleRolling, m) }
	sumRule := func(id *uint) dbmodels.ItemRuleVersion {
		return quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaMeasure = dbmodels.QuotaMeasureSum; v.QuotaOptionID = id })
	}
	targets := func(s string) mut { return func(v *dbmodels.ItemRuleVersion) { v.OptionTargets = s } }
	atMostSum := func(v *dbmodels.ItemRuleVersion) {
		v.QuotaComparator = dbmodels.QuotaAtMost
		v.QuotaMeasure = dbmodels.QuotaMeasureSum
	}
	tests := []struct {
		name string
		item ItemSpec
		rule dbmodels.ItemRuleVersion
		opts []OptionSpec
		want string // "" = ok
	}{
		{"unknown schedule", boolean, dbmodels.ItemRuleVersion{EffectiveFrom: "2026-11-07", ScheduleType: "weekly"}, nil, ReasonUnknownSchedule},
		{"empty schedule", boolean, dbmodels.ItemRuleVersion{EffectiveFrom: "2026-11-07"}, nil, ReasonUnknownSchedule},
		{"unknown tracker", ItemSpec{TrackerType: "x"}, none(nil), nil, ReasonUnknownTracker},
		{"unknown kind", ItemSpec{TrackerType: "boolean", Kind: "x"}, none(nil), nil, ReasonUnknownKind},
		{"unknown polarity", ItemSpec{TrackerType: "boolean", Polarity: "x"}, none(nil), nil, ReasonUnknownPolarity},
		{"event with none", ItemSpec{TrackerType: "boolean", Kind: "event"}, none(nil), nil, ""},
		{"event with rrule", ItemSpec{TrackerType: "boolean", Kind: "event"}, rrule(nil), nil, ""},
		{"event with quota", ItemSpec{TrackerType: "metric", Kind: "event"}, quota(nil), nil, ""},
		// quota rules
		{"quota sum metric", metric, sumRule(nil), nil, ""},
		{"quota sum counter", counter, sumRule(nil), nil, ""},
		{"quota sum boolean", boolean, sumRule(nil), nil, ReasonQuotaMeasure},
		{"quota days metric", metric, quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaMeasure = "days" }), nil, ""},
		{"quota bad measure", metric, quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaMeasure = "x" }), nil, ReasonQuotaMeasure},
		{"quota zero count", boolean, quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaCount = 0 }), nil, ReasonInvalidQuota},
		{"quota bad period", boolean, quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaPeriod = "day" }), nil, ReasonInvalidQuota},
		{"quota negative at_least", negBool, quota(nil), nil, ReasonInvalidQuota},
		{"negative metric sum", negMetric, quota(atMostSum), nil, ""},
		{"negative boolean sum", negBool, quota(atMostSum), nil, ReasonQuotaMeasure},
		{"quota default window", boolean, quota(nil), nil, ""},
		{"quota rolling window", boolean, quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaWindow = "rolling" }), nil, ""},
		// options and sum units (G19)
		{"sum two units no option", metric, sumRule(nil), two, ReasonQuotaOptionRequired},
		{"sum two units option set", metric, sumRule(uptr(2)), two, ""},
		{"sum same units no option", metric, sumRule(nil), sameUnit, ""},
		{"sum unknown option", metric, sumRule(uptr(9)), two, ReasonQuotaOptionUnknown},
		{"sum option without options", metric, sumRule(uptr(1)), nil, ReasonQuotaOptionUnknown},
		{"days measure with option id rejected", metric, quota(func(v *dbmodels.ItemRuleVersion) { v.QuotaOptionID = uptr(1) }), two, ReasonQuotaOptionNotApplic},
		{"sum measure with valid option id", metric, quota(func(v *dbmodels.ItemRuleVersion) {
			v.QuotaMeasure = dbmodels.QuotaMeasureSum
			v.QuotaOptionID = uptr(1)
		}), two, ""},
		{"sum measure with unknown option id", metric, quota(func(v *dbmodels.ItemRuleVersion) {
			v.QuotaMeasure = dbmodels.QuotaMeasureSum
			v.QuotaOptionID = uptr(7)
		}), two, ReasonQuotaOptionUnknown},
		{"options on boolean", boolean, none(nil), two, ReasonOptionsNotAllowed},
		{"options on maintenance", maint, none(nil), two, ReasonOptionsNotAllowed},
		{"options on counter", counter, none(nil), two, ""},
		{"duplicate option ids", counter, none(nil), []OptionSpec{opt(1, "a"), opt(1, "b")}, ReasonInvalidOption},
		{"zero option id", counter, none(nil), []OptionSpec{opt(0, "a")}, ReasonInvalidOption},
		{"option negative step", counter, none(nil), []OptionSpec{{ID: 1, Step: -1}}, ReasonInvalidOption},
		{"option nan step", counter, none(nil), []OptionSpec{{ID: 1, Step: math.NaN()}}, ReasonInvalidOption},
		{"option min>max", counter, none(nil), []OptionSpec{{ID: 1, MinVal: f64(5), MaxVal: f64(1)}}, ReasonInvalidOption},
		{"option bad aggregate", metric, none(nil), []OptionSpec{{ID: 1, Aggregate: "median"}}, ReasonInvalidOption},
		// completion
		{"completion any", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule = "any" }), two, ""},
		{"completion all", metric, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule = "all" }), two, ""},
		{"completion n=1", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule, v.CompletionN = "at_least_n", 1 }), two, ""},
		{"completion n=len", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule, v.CompletionN = "at_least_n", 2 }), two, ""},
		{"completion n=0", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule = "at_least_n" }), two, ReasonInvalidCompletion},
		{"completion n>len", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule, v.CompletionN = "at_least_n", 3 }), two, ReasonInvalidCompletion},
		{"completion n with any", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule, v.CompletionN = "any", 1 }), two, ReasonInvalidCompletion},
		{"completion bad enum", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule = "most" }), two, ReasonInvalidCompletion},
		{"completion no options", counter, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule = "any" }), nil, ReasonCompletionNotApplic},
		{"completion on boolean", boolean, none(func(v *dbmodels.ItemRuleVersion) { v.CompletionRule = "all" }), nil, ReasonCompletionNotApplic},
		// stale_after_days
		{"stale none", boolean, none(func(v *dbmodels.ItemRuleVersion) { v.StaleAfterDays = 30 }), nil, ""},
		{"stale negative", boolean, none(func(v *dbmodels.ItemRuleVersion) { v.StaleAfterDays = -1 }), nil, ReasonInvalidStaleAfter},
		{"stale on rrule", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.StaleAfterDays = 3 }), nil, ReasonInvalidStaleAfter},
		// foreign fields
		{"rolling fields on rrule", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.RollingInterval = 2 }), nil, ReasonForeignRollingFields},
		{"rolling unit on none", boolean, none(func(v *dbmodels.ItemRuleVersion) { v.RollingUnit = "day" }), nil, ReasonForeignRollingFields},
		{"rrule on rolling", boolean, rolling(func(v *dbmodels.ItemRuleVersion) { v.RRuleString = "FREQ=DAILY" }), nil, ReasonForeignRRuleFields},
		{"rrule on none", boolean, none(func(v *dbmodels.ItemRuleVersion) { v.RRuleString = "FREQ=DAILY" }), nil, ReasonForeignRRuleFields},
		{"until on quota", boolean, quota(func(v *dbmodels.ItemRuleVersion) { v.Until = new(string) }), nil, ReasonForeignRRuleFields},
		{"quota fields on rrule", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.QuotaCount = 2 }), nil, ReasonForeignQuotaFields},
		{"quota option on none", counter, none(func(v *dbmodels.ItemRuleVersion) { v.QuotaOptionID = uptr(1) }), two, ReasonForeignQuotaFields},
		{"holiday on quota", boolean, quota(func(v *dbmodels.ItemRuleVersion) { v.HolidayMode = "skip_holidays" }), nil, ReasonHolidayModeNotAllow},
		{"holiday on rrule", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.HolidayMode = "workdays_only" }), nil, ""},
		{"bad holiday", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.HolidayMode = "x" }), nil, ReasonInvalidRule},
		{"bad rrule", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.RRuleString = "FREQ=HOURLY" }), nil, ReasonInvalidRule},
		{"bad rolling interval", maint, rolling(func(v *dbmodels.ItemRuleVersion) { v.RollingInterval = 0 }), nil, ReasonInvalidRule},
		// targets
		{"target counter", counter, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(8) }), nil, ""},
		{"target zero", counter, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(0) }), nil, ""},
		{"target negative", counter, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(-1) }), nil, ReasonInvalidTarget},
		{"target nan", counter, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(math.NaN()) }), nil, ReasonInvalidTarget},
		{"target inf", metric, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(math.Inf(1)) }), nil, ReasonInvalidTarget},
		{"target on boolean", boolean, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(1) }), nil, ReasonTargetNotApplicable},
		{"target with options", counter, rrule(func(v *dbmodels.ItemRuleVersion) { v.TargetValue = f64(1) }), two, ReasonTargetNotApplicable},
		{"option targets ok", counter, rrule(targets(`{"1": 20, "2": 2.5}`)), two, ""},
		{"option targets unknown key", counter, rrule(targets(`{"9": 20}`)), two, ReasonInvalidOptionTargets},
		{"option targets non-numeric key", counter, rrule(targets(`{"a": 20}`)), two, ReasonInvalidOptionTargets},
		{"option targets negative", counter, rrule(targets(`{"1": -1}`)), two, ReasonInvalidOptionTargets},
		{"option targets string value", counter, rrule(targets(`{"1": "x"}`)), two, ReasonInvalidOptionTargets},
		{"option targets array", counter, rrule(targets(`[1]`)), two, ReasonInvalidOptionTargets},
		{"option targets null", counter, rrule(targets(`null`)), two, ReasonInvalidOptionTargets},
		{"option targets without options", counter, rrule(targets(`{"1": 2}`)), nil, ReasonInvalidOptionTargets},
		// numeric item fields
		{"item step negative", ItemSpec{TrackerType: "counter", Step: -1}, none(nil), nil, ReasonInvalidNumeric},
		{"item step on boolean", ItemSpec{TrackerType: "boolean", Step: 1}, none(nil), nil, ReasonInvalidNumeric},
		{"item min>max", ItemSpec{TrackerType: "metric", MinVal: f64(2), MaxVal: f64(1)}, none(nil), nil, ReasonInvalidNumeric},
		{"warning days maintenance", ItemSpec{TrackerType: "maintenance", WarningDays: 7}, rolling(nil), nil, ""},
		{"warning days negative", ItemSpec{TrackerType: "maintenance", WarningDays: -1}, rolling(nil), nil, ReasonInvalidWarningDays},
		{"warning days on boolean", ItemSpec{TrackerType: "boolean", WarningDays: 1}, none(nil), nil, ReasonInvalidWarningDays},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateItemRule(tt.item, tt.rule, tt.opts)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			if got := Reason(err); got != tt.want {
				t.Fatalf("reason = %q (%v), want %q", got, err, tt.want)
			}
		})
	}
}

func TestValidateRuleRejectsUnknownScheduleType(t *testing.T) {
	err := ValidateRule(dbmodels.ItemRuleVersion{EffectiveFrom: "2026-11-07", ScheduleType: "weekly"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	for _, st := range []dbmodels.ScheduleType{dbmodels.ScheduleNone, dbmodels.ScheduleQuota} {
		if err := ValidateRule(dbmodels.ItemRuleVersion{EffectiveFrom: "2026-11-07", ScheduleType: st}); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
}
