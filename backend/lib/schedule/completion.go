package schedule

import (
	"encoding/json"
	"strconv"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// OptionProgress is one option's (or the whole item's, when it has no options)
// progress on a single day.
type OptionProgress struct {
	// OptionID is nil for an item without options.
	OptionID  *uint
	Value     float64
	Target    float64
	HasTarget bool
	// Ratio is Value/Target (uncapped); 0 when there is no positive target.
	Ratio float64
	Met   bool
}

// Completion is the evaluated state of one item on one day.
type Completion struct {
	Done         bool
	Progress     []OptionProgress
	OptionsMet   int
	OptionsTotal int
}

// Shade is the heatmap shade: OptionsMet/OptionsTotal (0 when no options).
func (c Completion) Shade() float64 {
	if c.OptionsTotal == 0 {
		return 0
	}
	return float64(c.OptionsMet) / float64(c.OptionsTotal)
}

// DayCompletion evaluates one day (DESIGN 2/2.1). version must be the rule
// version in effect on that day: targets come from its snapshot, never from
// the live item. logs are the logs of that day only.
//
//   - boolean/maintenance: done when any log exists.
//   - counter: SUM(value) per option >= target.
//   - metric: per-option day value by aggregate (latest|avg|sum; latest is by
//     checkin_time then id) >= target.
//   - an option or item without a target is met as soon as it has a log.
//   - archived options are excluded; logs without an option (ad-hoc) do not
//     count towards any option of an item that has options.
//   - the completion rule (any|all|at_least_n) combines the options; an item
//     without options is a single pseudo-option using version.TargetValue.
//
// itemAgg is the aggregate used for a metric without options (default latest).
func DayCompletion(tracker dbmodels.TrackerType, v dbmodels.ItemRuleVersion, options []dbmodels.ItemOption,
	logs []dbmodels.CheckinLog, itemAgg dbmodels.Aggregate) Completion {
	if tracker == dbmodels.TrackerBoolean || tracker == dbmodels.TrackerMaintenance {
		met := 0
		if len(logs) > 0 {
			met = 1
		}
		return Completion{Done: met == 1, OptionsMet: met, OptionsTotal: 1}
	}

	targets := map[string]float64{}
	if v.OptionTargets != "" {
		_ = json.Unmarshal([]byte(v.OptionTargets), &targets) // malformed snapshot: no targets
	}

	var c Completion
	if len(options) == 0 {
		c.Progress = []OptionProgress{progressFor(tracker, nil, itemAgg, v.TargetValue, logs)}
	} else {
		for _, o := range options {
			if o.Archived {
				continue
			}
			var own []dbmodels.CheckinLog
			for _, l := range logs {
				if l.OptionID != nil && *l.OptionID == o.ID {
					own = append(own, l)
				}
			}
			var tgt *float64
			if t, ok := targets[strconv.FormatUint(uint64(o.ID), 10)]; ok {
				tgt = &t
			}
			id := o.ID
			agg := o.Aggregate
			if agg == "" {
				agg = itemAgg
			}
			c.Progress = append(c.Progress, progressFor(tracker, &id, agg, tgt, own))
		}
	}
	c.OptionsTotal = len(c.Progress)
	for _, p := range c.Progress {
		if p.Met {
			c.OptionsMet++
		}
	}
	switch {
	case c.OptionsTotal == 0:
	case v.CompletionRule == dbmodels.CompletionAll:
		c.Done = c.OptionsMet == c.OptionsTotal
	case v.CompletionRule == dbmodels.CompletionAtLeast:
		n := v.CompletionN
		if n < 1 {
			n = 1
		}
		c.Done = c.OptionsMet >= n
	default: // any (also the zero value)
		c.Done = c.OptionsMet >= 1
	}
	return c
}

func progressFor(tracker dbmodels.TrackerType, id *uint, agg dbmodels.Aggregate, target *float64, logs []dbmodels.CheckinLog) OptionProgress {
	p := OptionProgress{OptionID: id}
	if target != nil {
		p.Target, p.HasTarget = *target, true
	}
	if len(logs) == 0 {
		return p
	}
	if tracker == dbmodels.TrackerCounter {
		agg = dbmodels.AggregateSum
	}
	switch agg {
	case dbmodels.AggregateSum:
		for _, l := range logs {
			p.Value += l.Value
		}
	case dbmodels.AggregateAvg:
		for _, l := range logs {
			p.Value += l.Value
		}
		p.Value /= float64(len(logs))
	default: // latest
		last := logs[0]
		for _, l := range logs[1:] {
			if l.CheckinTime.After(last.CheckinTime) || (l.CheckinTime.Equal(last.CheckinTime) && l.ID > last.ID) {
				last = l
			}
		}
		p.Value = last.Value
	}
	if !p.HasTarget {
		p.Met = true
		return p
	}
	p.Met = p.Value >= p.Target
	if p.Target > 0 {
		p.Ratio = p.Value / p.Target
	}
	return p
}
