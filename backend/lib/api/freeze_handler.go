package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	maxFreezeDays   = 366
	maxFreezeReason = 255
)

type freezeResponse struct {
	ID        uint   `json:"id"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Reason    string `json:"reason"`
}

func newFreezeResponse(f dbmodels.FreezePeriod) freezeResponse {
	return freezeResponse{ID: f.ID, StartDate: f.StartDate, EndDate: f.EndDate, Reason: f.Reason}
}

type freezeRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Reason    string `json:"reason"`
}

func strictDate(s string) (time.Time, bool) {
	t, err := time.Parse(dbmodels.DateLayout, s)
	if err != nil || t.Format(dbmodels.DateLayout) != s || t.Year() < 1900 {
		return time.Time{}, false
	}
	return t, true
}

// GET /api/freezes: the user's freeze periods, newest start first.
func (s *server) listFreezes(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	var rows []dbmodels.FreezePeriod
	if err := s.db.Read().Where("user_id = ?", u.ID).Order("start_date DESC, id DESC").Find(&rows).Error; err != nil {
		log.Error().Err(err).Msg("list freezes failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	out := make([]freezeResponse, len(rows))
	for i, f := range rows {
		out[i] = newFreezeResponse(f)
	}
	jsonResult(http.StatusOK, map[string]any{"freezes": out}).write(w)
}

// POST /api/freezes: start_date and end_date inclusive. Bumps state_version.
func (s *server) postFreeze(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	var req freezeRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return
	}
	start, ok1 := strictDate(req.StartDate)
	end, ok2 := strictDate(req.EndDate)
	switch {
	case !ok1 || !ok2:
		errResult(http.StatusBadRequest, "start_date and end_date must be YYYY-MM-DD").write(w)
		return
	case end.Before(start):
		errResult(http.StatusBadRequest, "start_date must not be after end_date").write(w)
		return
	case int(end.Sub(start).Hours()/24)+1 > maxFreezeDays:
		errResult(http.StatusBadRequest, "freeze period too long (max 366 days)").write(w)
		return
	case utf8.RuneCountInString(req.Reason) > maxFreezeReason:
		errResult(http.StatusBadRequest, "reason too long").write(w)
		return
	}
	row := dbmodels.FreezePeriod{UserID: u.ID, StartDate: req.StartDate, EndDate: req.EndDate, Reason: req.Reason}
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return dbmodels.BumpStateVersion(tx, u.ID)
	})
	if err != nil {
		log.Error().Err(err).Msg("create freeze failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	jsonResult(http.StatusCreated, newFreezeResponse(row)).write(w)
}

// DELETE /api/freezes/{id}: removes the user's own freeze. Bumps state_version.
func (s *server) deleteFreeze(w http.ResponseWriter, r *http.Request) {
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
	found := false
	err = s.db.Write().Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", id, u.ID).Delete(&dbmodels.FreezePeriod{})
		if res.Error != nil {
			return res.Error
		}
		if found = res.RowsAffected > 0; !found {
			return nil
		}
		return dbmodels.BumpStateVersion(tx, u.ID)
	})
	if err != nil {
		log.Error().Err(err).Msg("delete freeze failed")
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	if !found {
		errResult(http.StatusNotFound, "freeze not found").write(w)
		return
	}
	jsonResult(http.StatusOK, map[string]bool{"ok": true}).write(w)
}
