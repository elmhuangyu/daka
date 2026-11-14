package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/stats"
)

type statsResponse struct {
	ItemID      uint   `json:"item_id"`
	Name        string `json:"name"`
	IsSensitive bool   `json:"is_sensitive"`
	stats.Stats
}

// GET /api/items/{id}/stats?redact=1 (DESIGN 11.2).
func (s *server) getItemStats(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		errResult(http.StatusBadRequest, "invalid id").write(w)
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	res, err := s.itemStats(u.ID, uint(id), redact, isReadToken(r))
	if err != nil {
		log.Error().Err(err).Msg("item stats failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	res.write(w)
}

func (s *server) itemStats(userID string, itemID uint, redact, hideSensitive bool) (result, error) {
	db := s.db.Read()
	item, _, found, err := loadAccessibleItem(db, userID, itemID)
	if err != nil {
		return result{}, err
	}
	if !found {
		return errResult(http.StatusNotFound, "item not found"), nil
	}
	val, err := s.cachedStats(db, userID, item)
	if err != nil {
		return result{}, err
	}

	sens, err := sensitiveFn(db, item.UserID)
	if err != nil {
		return result{}, err
	}
	sensitive := sens(item)
	if hideSensitive && sensitive {
		return errResult(http.StatusNotFound, "item not found"), nil
	}
	resp := statsResponse{ItemID: item.ID, Name: item.Name, IsSensitive: sensitive, Stats: val}
	if redact && sensitive {
		resp.Name = privateItemName
		// Option names can reveal the item too; the cached slice is not mutated.
		opts := make([]stats.OptionSummary, len(val.Options))
		for i, o := range val.Options {
			if o.OptionID != nil {
				o.Name = fmt.Sprintf("Option %d", i+1)
			}
			o.Unit = ""
			opts[i] = o
		}
		resp.Options = opts
	}
	return jsonResult(http.StatusOK, resp), nil
}

// cachedStats computes (or fetches from the cache) the stats of one owned item.
func (s *server) cachedStats(db *gorm.DB, userID string, item dbmodels.Item) (stats.Stats, error) {
	itemID := item.ID
	st, err := userSettings(db, userID)
	if err != nil {
		return stats.Stats{}, err
	}
	today, err := schedule.Today(s.now(), st.Timezone)
	if err != nil {
		return stats.Stats{}, err
	}
	logSig, err := stats.LogSignature(db, s.versions, itemID)
	if err != nil {
		return stats.Stats{}, err
	}
	ruleSig, err := stats.RuleSignature(db, itemID)
	if err != nil {
		return stats.Stats{}, err
	}
	key := stats.Key{
		UserID: userID, ItemID: itemID, LogSig: logSig, RuleSig: ruleSig, Today: today, StateVersion: st.StateVersion,
		SettingsSig: fmt.Sprintf("%s/%d/%s/%t", st.Timezone, st.WeekStart, st.HolidayRegion, st.StreakHolidayFreeze),
	}
	val, hit := s.cache.Get(key)
	if !hit {
		in := stats.Input{
			Item: item, Calendar: s.calendarFor(st), Today: today,
			Settings: stats.Settings{Timezone: st.Timezone, WeekStart: st.WeekStart, StreakHolidayFreeze: st.StreakHolidayFreeze},
		}
		if err := db.Where("item_id = ?", itemID).Find(&in.Versions).Error; err != nil {
			return stats.Stats{}, err
		}
		if err := db.Where("item_id = ?", itemID).Order("sort_order, id").Find(&in.Options).Error; err != nil {
			return stats.Stats{}, err
		}
		if err := db.Where("item_id = ?", itemID).Find(&in.Logs).Error; err != nil {
			return stats.Stats{}, err
		}
		if err := db.Where("user_id = ?", userID).Find(&in.Freezes).Error; err != nil {
			return stats.Stats{}, err
		}
		if val, err = stats.ComputeStats(in); err != nil {
			return stats.Stats{}, err
		}
		s.cache.Put(key, val)
	}

	return val, nil
}
