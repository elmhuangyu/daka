package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const (
	maxBodyBytes      = 1 << 20
	maxIdempotencyKey = 255
	maxNoteBytes      = 2000
)

type checkinRequest struct {
	ItemID   uint    `json:"item_id"`
	OptionID *uint   `json:"option_id"`
	Date     string  `json:"date"`
	Mode     string  `json:"mode"`
	Value    float64 `json:"value"`
	Note     string  `json:"note"`
}

type checkinResponse struct {
	ID           uint   `json:"id"`
	ItemID       uint   `json:"item_id"`
	Date         string `json:"date"`
	Mode         string `json:"mode"`
	IsSupplement bool   `json:"is_supplement"`
	Note         string `json:"note"`
}

type unsetResponse struct {
	OK      bool   `json:"ok"`
	ItemID  uint   `json:"item_id"`
	Date    string `json:"date"`
	Mode    string `json:"mode"`
	Deleted int64  `json:"deleted"`
}

func (s *server) postCheckin(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) > maxIdempotencyKey {
		errResult(http.StatusBadRequest, "Idempotency-Key too long").write(w)
		return
	}
	var req checkinRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return
	}

	res, err := s.execute(u.ID, key, req)
	if err != nil {
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	res.write(w)
}

// execute runs the check-in inside one write transaction. With a key, the
// lookup, the write and the key insert share that transaction: on SQLite the
// single write connection serializes concurrent requests, on PostgreSQL a
// concurrent duplicate fails on the primary key and is resolved by re-reading
// the winner's stored response.
func (s *server) execute(userID, key string, req checkinRequest) (result, error) {
	var res result
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		if key != "" {
			var stored dbmodels.IdempotencyKey
			err := tx.Where("user_id = ? AND key = ?", userID, key).Take(&stored).Error
			if err == nil {
				res = result{status: stored.StatusCode, body: []byte(stored.ResponseBody)}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		var err error
		res, err = s.applyCheckin(tx, userID, req)
		if err != nil {
			return err
		}
		if key != "" && res.status >= 200 && res.status < 300 {
			return tx.Create(&dbmodels.IdempotencyKey{
				UserID: userID, Key: key, StatusCode: res.status,
				ResponseBody: string(res.body), CreatedAt: s.now().UTC(),
			}).Error
		}
		return nil
	})
	if err != nil && key != "" {
		// PostgreSQL only: a concurrent request with the same key committed
		// first and our key insert hit the primary key; return its stored
		// response. (Double-apply of the check-in itself is prevented by the
		// advisory lock in applyCheckin.)
		var stored dbmodels.IdempotencyKey
		if e := s.db.Write().Where("user_id = ? AND key = ?", userID, key).Take(&stored).Error; e == nil {
			return result{status: stored.StatusCode, body: []byte(stored.ResponseBody)}, nil
		}
	}
	return res, err
}

// userTimezone returns the IANA timezone name of the user (default UTC).
func userTimezone(tx *gorm.DB, userID string) (string, error) {
	var st dbmodels.UserSettings
	err := tx.Where("user_id = ?", userID).Take(&st).Error
	switch {
	case err == nil:
		if st.Timezone != "" {
			return st.Timezone, nil
		}
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return "", err
	}
	return "UTC", nil
}

// userToday returns the user's local date (YYYY-MM-DD) in UserSettings.timezone.
func (s *server) userToday(tx *gorm.DB, userID string) (string, error) {
	tz, err := userTimezone(tx, userID)
	if err != nil {
		return "", err
	}
	return schedule.Today(s.now(), tz)
}

// applyCheckin validates and applies one request. Business failures are
// returned as results (nil error); only infrastructure errors are errors.
func (s *server) applyCheckin(tx *gorm.DB, userID string, req checkinRequest) (result, error) {
	switch req.Mode {
	case "set", "unset", "delta", "append":
	default:
		return errResult(http.StatusBadRequest, "invalid mode"), nil
	}
	if len(req.Note) > maxNoteBytes {
		return errResult(http.StatusBadRequest, "note too long"), nil
	}
	today, err := s.userToday(tx, userID)
	if err != nil {
		return result{}, err
	}
	date := req.Date
	if date == "" {
		date = today
	} else if t, perr := time.Parse(dbmodels.DateLayout, date); perr != nil || t.Format(dbmodels.DateLayout) != date {
		return errResult(http.StatusBadRequest, "invalid date, expected YYYY-MM-DD"), nil
	}
	if date > today {
		return errResult(http.StatusBadRequest, "date is in the future"), nil
	}

	var item dbmodels.Item
	err = tx.Where("id = ? AND user_id = ?", req.ItemID, userID).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errResult(http.StatusNotFound, "item not found"), nil
	}
	if err != nil {
		return result{}, err
	}
	if item.Archived {
		return errResult(http.StatusBadRequest, "item is archived"), nil
	}
	if item.TrackerType != dbmodels.TrackerBoolean || req.OptionID != nil {
		return errResult(http.StatusBadRequest, "not supported yet"), nil
	}
	if item.Polarity == dbmodels.PolarityNegative {
		if req.Mode == "append" {
			return errResult(http.StatusBadRequest, "not supported yet"), nil
		}
		return errResult(http.StatusBadRequest, "mode does not match item type"), nil
	}
	if req.Mode == "delta" || req.Mode == "append" {
		return errResult(http.StatusBadRequest, "mode does not match item type"), nil
	}

	// Serialize concurrent writers per (user, item, date). SQLite needs
	// nothing: its single write connection already serializes transactions.
	// PostgreSQL runs READ COMMITTED and the table has no unique index, so two
	// concurrent first-time sets could both insert without this lock.
	if needsAdvisoryLock(tx.Name()) {
		lockKey := fmt.Sprintf("%s|%d|%s", userID, item.ID, date)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", lockKey).Error; err != nil {
			return result{}, err
		}
	}

	var logs []dbmodels.CheckinLog
	err = tx.Where("user_id = ? AND item_id = ? AND checkin_date = ?", userID, item.ID, date).
		Order("id").Find(&logs).Error
	if err != nil {
		return result{}, err
	}

	if req.Mode == "unset" {
		var n int64
		if len(logs) > 0 {
			tr := tx.Where("user_id = ? AND item_id = ? AND checkin_date = ?", userID, item.ID, date).
				Delete(&dbmodels.CheckinLog{})
			if tr.Error != nil {
				return result{}, tr.Error
			}
			n = tr.RowsAffected
		}
		return jsonResult(http.StatusOK, unsetResponse{OK: true, ItemID: item.ID, Date: date, Mode: "unset", Deleted: n}), nil
	}

	var row dbmodels.CheckinLog
	if len(logs) == 0 {
		row = dbmodels.CheckinLog{
			UserID: userID, ItemID: item.ID, CheckinDate: date,
			CheckinTime: s.now().UTC(), Note: req.Note,
			Source: dbmodels.SourceWeb, IsSupplement: date < today,
		}
		if err := tx.Create(&row).Error; err != nil {
			return result{}, err
		}
	} else {
		row = logs[0]
		row.Note = req.Note
		if err := tx.Model(&dbmodels.CheckinLog{}).Where("id = ?", row.ID).Update("note", req.Note).Error; err != nil {
			return result{}, err
		}
		if len(logs) > 1 { // heal legacy duplicates
			if err := tx.Where("id <> ? AND user_id = ? AND item_id = ? AND checkin_date = ?", row.ID, userID, item.ID, date).
				Delete(&dbmodels.CheckinLog{}).Error; err != nil {
				return result{}, err
			}
		}
	}
	return jsonResult(http.StatusOK, checkinResponse{
		ID: row.ID, ItemID: item.ID, Date: date, Mode: "set",
		IsSupplement: row.IsSupplement, Note: row.Note,
	}), nil
}

func (s *server) deleteCheckin(w http.ResponseWriter, r *http.Request) {
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
	tr := s.db.Write().Where("id = ? AND user_id = ?", id, u.ID).Delete(&dbmodels.CheckinLog{})
	if tr.Error != nil {
		errResult(http.StatusInternalServerError, "internal error").write(w)
		return
	}
	if tr.RowsAffected == 0 {
		errResult(http.StatusNotFound, "check-in not found").write(w)
		return
	}
	jsonResult(http.StatusOK, map[string]bool{"ok": true}).write(w)
}

// needsAdvisoryLock reports whether the dialect needs an explicit per-row-set
// lock to serialize writers (PostgreSQL; SQLite has one write connection).
func needsAdvisoryLock(dialect string) bool { return dialect == "postgres" }
