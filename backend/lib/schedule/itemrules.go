package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// Stable machine-readable reason codes returned (via Reason) by
// ValidateItemRule. They are part of the HTTP API contract.
const (
	ReasonUnknownTracker       = "unknown_tracker_type"
	ReasonUnknownKind          = "unknown_kind"
	ReasonUnknownPolarity      = "unknown_polarity"
	ReasonUnknownSchedule      = "unknown_schedule_type"
	ReasonScheduleNotAllowed   = "schedule_not_allowed_for_tracker"
	ReasonNegativeTracker      = "negative_tracker_not_allowed"
	ReasonNegativeSchedule     = "negative_schedule_not_allowed"
	ReasonInvalidRule          = "invalid_rule"
	ReasonForeignRRuleFields   = "foreign_rrule_fields"
	ReasonForeignRollingFields = "foreign_rolling_fields"
	ReasonForeignQuotaFields   = "foreign_quota_fields"
	ReasonHolidayModeNotAllow  = "holiday_mode_not_allowed"
	ReasonInvalidQuota         = "invalid_quota"
	ReasonQuotaMeasure         = "invalid_quota_measure"
	ReasonQuotaOptionRequired  = "quota_option_required"
	ReasonQuotaOptionUnknown   = "quota_option_unknown"
	ReasonQuotaOptionNotApplic = "quota_option_not_applicable"
	ReasonInvalidStaleAfter    = "invalid_stale_after_days"
	ReasonInvalidTarget        = "invalid_target_value"
	ReasonTargetNotApplicable  = "target_not_applicable"
	ReasonOptionsNotAllowed    = "options_not_allowed"
	ReasonInvalidOption        = "invalid_option"
	ReasonInvalidOptionTargets = "invalid_option_targets"
	ReasonInvalidCompletion    = "invalid_completion_rule"
	ReasonCompletionNotApplic  = "completion_rule_not_applicable"
	ReasonInvalidWarningDays   = "invalid_warning_days"
	ReasonInvalidNumeric       = "invalid_numeric_field"
)

// ReasonError is a validation failure with a stable reason code. It wraps
// ErrInvalid.
type ReasonError struct {
	Code string
	Msg  string
}

func (e *ReasonError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrInvalid.Error(), e.Code, e.Msg)
}
func (e *ReasonError) Unwrap() error { return ErrInvalid }

func reasonf(code, format string, args ...any) error {
	return &ReasonError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Reason returns the reason code of an ErrInvalid failure; ReasonInvalidRule
// when the error carries no specific code.
func Reason(err error) string {
	var re *ReasonError
	if errors.As(err, &re) {
		return re.Code
	}
	return ReasonInvalidRule
}

func trimInvalid(err error) string {
	return strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
}

// ItemSpec is the item-level input of ValidateItemRule. Empty Kind means ongoing
// and empty Polarity means positive.
type ItemSpec struct {
	TrackerType dbmodels.TrackerType
	Kind        dbmodels.Kind
	Polarity    dbmodels.Polarity
	// Item-level numeric fields (zero / nil = unset).
	Unit        string
	Step        float64
	MinVal      *float64
	MaxVal      *float64
	Aggregate   dbmodels.Aggregate
	WarningDays int
}

// OptionSpec describes one option of a counter/metric item. ID is the option
// id (or a placeholder at creation time) that option_targets and
// quota_option_id refer to. Step 0 means unset.
type OptionSpec struct {
	ID        uint
	Unit      string
	Step      float64
	MinVal    *float64
	MaxVal    *float64
	Aggregate dbmodels.Aggregate
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// ValidateItemRule is the single authority for the tracker_type x
// schedule_type x polarity x kind matrix (DESIGN 2), quota measure rules,
// option/completion rules and foreign-field rejection. It is pure. Every
// failure wraps ErrInvalid and carries a Reason code.
func ValidateItemRule(item ItemSpec, rule dbmodels.ItemRuleVersion, options []OptionSpec) error {
	tracker := item.TrackerType
	switch tracker {
	case dbmodels.TrackerBoolean, dbmodels.TrackerCounter, dbmodels.TrackerMaintenance, dbmodels.TrackerMetric:
	default:
		return reasonf(ReasonUnknownTracker, "tracker_type must be boolean, counter, maintenance or metric")
	}
	switch item.Kind {
	case "", dbmodels.KindOngoing, dbmodels.KindEvent:
	default:
		return reasonf(ReasonUnknownKind, "kind must be ongoing or event")
	}
	polarity := item.Polarity
	switch polarity {
	case "":
		polarity = dbmodels.PolarityPositive
	case dbmodels.PolarityPositive, dbmodels.PolarityNegative:
	default:
		return reasonf(ReasonUnknownPolarity, "polarity must be positive or negative")
	}
	st := rule.ScheduleType
	switch st {
	case dbmodels.ScheduleFixedRRule, dbmodels.ScheduleRolling, dbmodels.ScheduleQuota, dbmodels.ScheduleNone:
	default:
		return reasonf(ReasonUnknownSchedule, "schedule_type must be fixed_rrule, rolling, quota or none")
	}

	// Type x schedule matrix.
	numeric := tracker == dbmodels.TrackerCounter || tracker == dbmodels.TrackerMetric
	if numeric && st == dbmodels.ScheduleRolling || tracker == dbmodels.TrackerMaintenance && st == dbmodels.ScheduleQuota {
		return reasonf(ReasonScheduleNotAllowed, "%s items cannot use a %s schedule", tracker, st)
	}
	if polarity == dbmodels.PolarityNegative {
		if tracker != dbmodels.TrackerBoolean && tracker != dbmodels.TrackerMetric {
			return reasonf(ReasonNegativeTracker, "negative items must be boolean or metric")
		}
		if st != dbmodels.ScheduleQuota && st != dbmodels.ScheduleNone {
			return reasonf(ReasonNegativeSchedule, "negative items only allow quota or none schedules")
		}
	}

	if err := validateItemNumbers(item, tracker); err != nil {
		return err
	}

	// Foreign fields and schedule-specific syntax.
	if st != dbmodels.ScheduleFixedRRule && (rule.RRuleString != "" || rule.Until != nil) {
		return reasonf(ReasonForeignRRuleFields, "rrule_string/until only apply to fixed_rrule")
	}
	if st != dbmodels.ScheduleFixedRRule && st != dbmodels.ScheduleRolling && rule.DtStart != "" {
		return reasonf(ReasonForeignRRuleFields, "dtstart only applies to fixed_rrule and rolling")
	}
	if st != dbmodels.ScheduleFixedRRule && rule.HolidayMode != "" && rule.HolidayMode != dbmodels.HolidayNone {
		return reasonf(ReasonHolidayModeNotAllow, "holiday_mode only applies to fixed_rrule")
	}
	if st != dbmodels.ScheduleRolling && (rule.RollingInterval != 0 || rule.RollingUnit != "") {
		return reasonf(ReasonForeignRollingFields, "rolling fields only apply to rolling")
	}
	if st != dbmodels.ScheduleQuota && (rule.QuotaComparator != "" || rule.QuotaCount != 0 || rule.QuotaPeriod != "" ||
		rule.QuotaWindow != "" || rule.QuotaMeasure != "" || rule.QuotaOptionID != nil) {
		return reasonf(ReasonForeignQuotaFields, "quota fields only apply to quota")
	}
	if rule.StaleAfterDays < 0 || (st != dbmodels.ScheduleNone && rule.StaleAfterDays != 0) {
		return reasonf(ReasonInvalidStaleAfter, "stale_after_days must be >= 0 and only applies to schedule none")
	}
	if err := ValidateRule(rule); err != nil {
		return reasonf(ReasonInvalidRule, "%s", trimInvalid(err))
	}

	opts, err := validateOptions(tracker, options)
	if err != nil {
		return err
	}
	if err := validateTargets(tracker, rule, opts); err != nil {
		return err
	}
	if err := validateCompletion(rule, len(options)); err != nil {
		return err
	}
	if st == dbmodels.ScheduleQuota {
		return validateQuotaRule(tracker, polarity, rule, options)
	}
	return nil
}

func validateItemNumbers(item ItemSpec, tracker dbmodels.TrackerType) error {
	numeric := tracker == dbmodels.TrackerCounter || tracker == dbmodels.TrackerMetric
	if !numeric && (item.Unit != "" || item.Step != 0 || item.MinVal != nil || item.MaxVal != nil || item.Aggregate != "") {
		return reasonf(ReasonInvalidNumeric, "unit/step/min_val/max_val/aggregate only apply to counter and metric")
	}
	if msg := checkNumeric(item.Step, item.MinVal, item.MaxVal, item.Aggregate); msg != "" {
		return reasonf(ReasonInvalidNumeric, "%s", msg)
	}
	if item.WarningDays < 0 || (tracker != dbmodels.TrackerMaintenance && item.WarningDays != 0) {
		return reasonf(ReasonInvalidWarningDays, "warning_days must be >= 0 and only applies to maintenance")
	}
	return nil
}

func checkNumeric(step float64, minV, maxV *float64, agg dbmodels.Aggregate) string {
	if !finite(step) || step < 0 {
		return "step must be a finite number > 0 when set"
	}
	if (minV != nil && !finite(*minV)) || (maxV != nil && !finite(*maxV)) {
		return "min_val/max_val must be finite"
	}
	if minV != nil && maxV != nil && *minV > *maxV {
		return "min_val must be <= max_val"
	}
	switch agg {
	case "", dbmodels.AggregateSum, dbmodels.AggregateLatest, dbmodels.AggregateAvg:
	default:
		return "aggregate must be sum, latest or avg"
	}
	return ""
}

func validateOptions(tracker dbmodels.TrackerType, options []OptionSpec) (map[uint]bool, error) {
	if len(options) == 0 {
		return nil, nil
	}
	if tracker != dbmodels.TrackerCounter && tracker != dbmodels.TrackerMetric {
		return nil, reasonf(ReasonOptionsNotAllowed, "options only apply to counter and metric items")
	}
	ids := make(map[uint]bool, len(options))
	for _, o := range options {
		if o.ID == 0 || ids[o.ID] {
			return nil, reasonf(ReasonInvalidOption, "option ids must be set and unique")
		}
		if msg := checkNumeric(o.Step, o.MinVal, o.MaxVal, o.Aggregate); msg != "" {
			return nil, reasonf(ReasonInvalidOption, "option: %s", msg)
		}
		ids[o.ID] = true
	}
	return ids, nil
}

func validateTargets(tracker dbmodels.TrackerType, rule dbmodels.ItemRuleVersion, opts map[uint]bool) error {
	if rule.TargetValue != nil {
		t := *rule.TargetValue
		if !finite(t) || t < 0 {
			return reasonf(ReasonInvalidTarget, "target_value must be a finite number >= 0")
		}
		if tracker != dbmodels.TrackerCounter && tracker != dbmodels.TrackerMetric {
			return reasonf(ReasonTargetNotApplicable, "target_value only applies to counter and metric")
		}
		if len(opts) > 0 {
			return reasonf(ReasonTargetNotApplicable, "items with options use per-option targets")
		}
	}
	if strings.TrimSpace(rule.OptionTargets) == "" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rule.OptionTargets), &raw); err != nil || raw == nil {
		return reasonf(ReasonInvalidOptionTargets, "option_targets must be a JSON object")
	}
	for k, v := range raw {
		id, err := strconv.ParseUint(k, 10, 64)
		if err != nil || !opts[uint(id)] {
			return reasonf(ReasonInvalidOptionTargets, "option_targets keys must be existing option ids")
		}
		var f float64
		if err := json.Unmarshal(v, &f); err != nil || !finite(f) || f < 0 {
			return reasonf(ReasonInvalidOptionTargets, "option target must be a finite number >= 0")
		}
	}
	return nil
}

func validateCompletion(rule dbmodels.ItemRuleVersion, nOptions int) error {
	if rule.CompletionRule == "" && rule.CompletionN == 0 {
		return nil
	}
	if nOptions == 0 {
		return reasonf(ReasonCompletionNotApplic, "completion_rule only applies to counter/metric items with options")
	}
	switch rule.CompletionRule {
	case "", dbmodels.CompletionAny, dbmodels.CompletionAll:
		if rule.CompletionN != 0 {
			return reasonf(ReasonInvalidCompletion, "completion_n only applies to at_least_n")
		}
	case dbmodels.CompletionAtLeast:
		if rule.CompletionN < 1 || rule.CompletionN > nOptions {
			return reasonf(ReasonInvalidCompletion, "completion_n must be between 1 and the number of options")
		}
	default:
		return reasonf(ReasonInvalidCompletion, "completion_rule must be any, all or at_least_n")
	}
	return nil
}

func validateQuotaRule(tracker dbmodels.TrackerType, polarity dbmodels.Polarity, rule dbmodels.ItemRuleVersion, options []OptionSpec) error {
	measure := EffectiveMeasure(rule.QuotaMeasure, polarity)
	if err := ValidateQuota(rule, tracker, polarity); err != nil {
		code := ReasonInvalidQuota
		if strings.Contains(err.Error(), "quota_measure") {
			code = ReasonQuotaMeasure
		}
		return reasonf(code, "%s", trimInvalid(err))
	}
	if rule.QuotaOptionID != nil {
		if measure != dbmodels.QuotaMeasureSum {
			return reasonf(ReasonQuotaOptionNotApplic, "quota_option_id only applies to quota_measure sum")
		}
		for _, o := range options {
			if o.ID == *rule.QuotaOptionID {
				return nil
			}
		}
		return reasonf(ReasonQuotaOptionUnknown, "quota_option_id does not match an option of this item")
	}
	if measure == dbmodels.QuotaMeasureSum {
		units := map[string]bool{}
		for _, o := range options {
			units[strings.TrimSpace(o.Unit)] = true
		}
		if len(units) > 1 {
			return reasonf(ReasonQuotaOptionRequired, "sum over options with different units requires quota_option_id")
		}
	}
	return nil
}
