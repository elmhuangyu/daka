package notify

import (
	"sort"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// entry is one item that still needs doing today.
type entry struct {
	ID         uint
	Name       string
	Sensitive  bool
	RemindTime string // "HH:MM" or empty
	// OverdueDays > 0 for a rolling item past its next due date.
	OverdueDays int
}

// frozenOn reports whether date lies in one of the user's freeze periods
// (reminders are dormant then, DESIGN 7.5).
func frozenOn(g *gorm.DB, userID, date string) (bool, error) {
	var n int64
	err := g.Model(&dbmodels.FreezePeriod{}).
		Where("user_id = ? AND start_date <= ? AND end_date >= ?", userID, date, date).Count(&n).Error
	return n > 0, err
}

// sensitiveFn returns whether an item is sensitive.
func sensitiveFn(g *gorm.DB, userID string) (func(dbmodels.Item) bool, error) {
	var cats []dbmodels.Category
	if err := g.Where("user_id = ? AND is_sensitive = ?", userID, true).Find(&cats).Error; err != nil {
		return nil, err
	}
	catSet := map[uint]bool{}
	for _, c := range cats {
		catSet[c.ID] = true
	}
	var projs []dbmodels.Project
	if err := g.Where("user_id = ?", userID).Find(&projs).Error; err != nil {
		return nil, err
	}
	projSet := map[uint]bool{}
	for _, p := range projs {
		if p.IsSensitive || (p.CategoryID != nil && catSet[*p.CategoryID]) {
			projSet[p.ID] = true
		}
	}
	return func(it dbmodels.Item) bool {
		return it.IsSensitive || (it.CategoryID != nil && catSet[*it.CategoryID]) || (it.ProjectID != nil && projSet[*it.ProjectID])
	}, nil
}

// pendingToday lists what is due on date (a local YYYY-MM-DD) and not yet
// done: fixed_rrule items on a scheduled day (holiday calendar included) and
// rolling items on or after their next due date. Quota, none, negative and
// archived items never remind. Broken rules are skipped, logging ids only.
func pendingToday(g *gorm.DB, userID, date string, cal schedule.DayCalendar) ([]entry, error) {
	var items []dbmodels.Item
	if err := g.Where("user_id = ? AND archived = ? AND polarity = ?", userID, false, dbmodels.PolarityPositive).
		Order("sort_order, id").Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	ids := make([]uint, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	var versions []dbmodels.ItemRuleVersion
	if err := g.Where("item_id IN ? AND effective_from <= ?", ids, date).Find(&versions).Error; err != nil {
		return nil, err
	}
	byItem := map[uint][]dbmodels.ItemRuleVersion{}
	for _, v := range versions {
		byItem[v.ItemID] = append(byItem[v.ItemID], v)
	}
	var logs []dbmodels.CheckinLog
	if err := g.Where("user_id = ? AND item_id IN ?", userID, ids).Find(&logs).Error; err != nil {
		return nil, err
	}
	dayLogs := map[uint][]dbmodels.CheckinLog{}
	allDates := map[uint][]string{}
	for _, l := range logs {
		allDates[l.ItemID] = append(allDates[l.ItemID], l.CheckinDate)
		if l.CheckinDate == date {
			dayLogs[l.ItemID] = append(dayLogs[l.ItemID], l)
		}
	}
	var options []dbmodels.ItemOption
	if err := g.Where("item_id IN ?", ids).Find(&options).Error; err != nil {
		return nil, err
	}
	optsByItem := map[uint][]dbmodels.ItemOption{}
	for _, o := range options {
		optsByItem[o.ItemID] = append(optsByItem[o.ItemID], o)
	}
	sens, err := sensitiveFn(g, userID)
	if err != nil {
		return nil, err
	}

	var out []entry
	for _, it := range items {
		vs := byItem[it.ID]
		cur := schedule.CurrentVersion(vs, date)
		if cur == nil {
			continue
		}
		e := entry{ID: it.ID, Name: it.Name, Sensitive: sens(it)}
		if it.RemindTime != nil {
			e.RemindTime = *it.RemindTime
		}
		switch cur.ScheduleType {
		case dbmodels.ScheduleFixedRRule:
			due, err := schedule.IsDueWith(vs, date, cal)
			if err != nil {
				log.Warn().Err(err).Uint("item_id", it.ID).Msg("notify: skipping item with unevaluable rule")
				continue
			}
			if !due && it.Kind == dbmodels.KindEvent {
				dayTime, _ := time.Parse(dbmodels.DateLayout, date)
				lookback := dayTime.AddDate(0, 0, -60).Format(dbmodels.DateLayout)
				yesterday := dayTime.AddDate(0, 0, -1).Format(dbmodels.DateLayout)
				pastDates, pErr := schedule.DueDatesWith(vs, lookback, yesterday, cal)
				if pErr == nil && len(pastDates) > 0 {
					lastDue := pastDates[len(pastDates)-1]
					var logCount int64
					if err := g.Model(&dbmodels.CheckinLog{}).
						Where("user_id = ? AND item_id = ? AND checkin_date >= ?", userID, it.ID, pastDates[0]).
						Count(&logCount).Error; err == nil && logCount == 0 {
						due = true
						e.OverdueDays = daysBetween(lastDue, date)
					}
				}
			}
			if !due {
				continue
			}
			if len(dayLogs[it.ID]) > 0 &&
				schedule.DayCompletion(it.TrackerType, *cur, optsByItem[it.ID], dayLogs[it.ID], it.Aggregate).Done {
				continue
			}
		case dbmodels.ScheduleRolling:
			next, err := schedule.NextDue(*cur, allDates[it.ID])
			if err != nil {
				log.Warn().Err(err).Uint("item_id", it.ID).Msg("notify: skipping item with unevaluable rule")
				continue
			}
			if date < next {
				continue
			}
			e.OverdueDays = daysBetween(next, date)
		default:
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OverdueDays == 0 && out[j].OverdueDays > 0 })
	return out, nil
}

func daysBetween(from, to string) int {
	a, err1 := time.Parse(dbmodels.DateLayout, from)
	b, err2 := time.Parse(dbmodels.DateLayout, to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}
