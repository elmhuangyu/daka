package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
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
	ItemID   uint     `json:"item_id"`
	OptionID *uint    `json:"option_id"`
	Date     string   `json:"date"`
	Mode     string   `json:"mode"`
	Value    *float64 `json:"value"`
	Note     string   `json:"note"`
	// Adhoc marks a one-off option for an item that has options: no option_id,
	// the free-form name goes in Note. It counts towards no option.
	Adhoc bool `json:"adhoc"`
}

type checkinResponse struct {
	ID           uint     `json:"id"`
	ItemID       uint     `json:"item_id"`
	Date         string   `json:"date"`
	Mode         string   `json:"mode"`
	IsSupplement bool     `json:"is_supplement"`
	Note         string   `json:"note"`
	OptionID     *uint    `json:"option_id,omitempty"`
	Value        *float64 `json:"value,omitempty"`
	// Total is the day's SUM(value) of the option after a delta.
	Total *float64 `json:"total,omitempty"`
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
	if !modeAllowed(item, req.Mode) {
		return errResult(http.StatusBadRequest, "mode does not match item type"), nil
	}
	opt, errRes, err := resolveOption(tx, item, req)
	if err != nil {
		return result{}, err
	}
	if errRes != nil {
		return *errRes, nil
	}
	if errRes := validateValue(item, opt, req); errRes != nil {
		return *errRes, nil
	}

	// Serialize concurrent writers per (user, item, date). SQLite needs
	// nothing: its single write connection already serializes transactions.
	// PostgreSQL runs READ COMMITTED and the table has no unique index, so two
	// concurrent first-time sets (or deltas racing the non-negative guard)
	// could both pass without this lock. Plain appends need no guard.
	if needsAdvisoryLock(tx.Name()) && req.Mode != "append" {
		lockKey := fmt.Sprintf("%s|%d|%s", userID, item.ID, date)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", lockKey).Error; err != nil {
			return result{}, err
		}
	}

	switch req.Mode {
	case "delta":
		return s.applyDelta(tx, userID, item, opt, date, today, req)
	case "append":
		return s.applyAppend(tx, userID, item, opt, date, today, req)
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

// maxAbsValue bounds any submitted value.
const maxAbsValue = 1e9

// modeAllowed is the mode/type matrix: boolean positive set|unset, boolean
// negative append, counter delta, maintenance set|unset, metric append.
func modeAllowed(item dbmodels.Item, mode string) bool {
	switch item.TrackerType {
	case dbmodels.TrackerBoolean:
		if item.Polarity == dbmodels.PolarityNegative {
			return mode == "append"
		}
		return mode == "set" || mode == "unset"
	case dbmodels.TrackerMaintenance:
		return mode == "set" || mode == "unset"
	case dbmodels.TrackerCounter:
		return mode == "delta"
	case dbmodels.TrackerMetric:
		return mode == "append"
	}
	return false
}

// resolveOption validates option_id/adhoc against the item's non-archived
// options. Only counter and metric items have options. With options, option_id
// is required (or adhoc:true with a name in note and no option_id); without
// options neither may be sent. It returns the option to log against (nil for
// none or ad-hoc).
func resolveOption(tx *gorm.DB, item dbmodels.Item, req checkinRequest) (*dbmodels.ItemOption, *result, error) {
	bad := func(msg string) (*dbmodels.ItemOption, *result, error) {
		r := errResult(http.StatusBadRequest, msg)
		return nil, &r, nil
	}
	var opts []dbmodels.ItemOption
	if item.TrackerType == dbmodels.TrackerCounter || item.TrackerType == dbmodels.TrackerMetric {
		if err := tx.Where("item_id = ? AND archived = ?", item.ID, false).Find(&opts).Error; err != nil {
			return nil, nil, err
		}
	}
	if len(opts) == 0 {
		if req.OptionID != nil || req.Adhoc {
			return bad("item has no options")
		}
		return nil, nil, nil
	}
	if req.Adhoc {
		if req.OptionID != nil {
			return bad("adhoc check-ins must not carry option_id")
		}
		if strings.TrimSpace(req.Note) == "" {
			return bad("adhoc check-ins need the option name in note")
		}
		return nil, nil, nil
	}
	if req.OptionID == nil {
		return bad("option_id is required")
	}
	for i := range opts {
		if opts[i].ID == *req.OptionID {
			return &opts[i], nil, nil
		}
	}
	return bad("invalid option_id")
}

// validateValue checks the numeric payload of delta/append requests.
func validateValue(item dbmodels.Item, opt *dbmodels.ItemOption, req checkinRequest) *result {
	bad := func(msg string) *result {
		r := errResult(http.StatusBadRequest, msg)
		return &r
	}
	if req.Mode != "delta" && req.Mode != "append" {
		return nil
	}
	if item.TrackerType == dbmodels.TrackerBoolean { // negative boolean: value ignored
		return nil
	}
	if req.Value == nil {
		return bad("value is required")
	}
	v := *req.Value
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxAbsValue {
		return bad("value out of range")
	}
	if req.Mode == "delta" {
		if v == 0 {
			return bad("delta must not be zero")
		}
		return nil
	}
	if item.MinVal != nil && v < *item.MinVal || item.MaxVal != nil && v > *item.MaxVal {
		return bad("value out of range")
	}
	if opt != nil && (opt.MinVal != nil && v < *opt.MinVal || opt.MaxVal != nil && v > *opt.MaxVal) {
		return bad("value out of range")
	}
	return nil
}

func optionEq(q *gorm.DB, id *uint) *gorm.DB {
	if id == nil {
		return q.Where("option_id IS NULL")
	}
	return q.Where("option_id = ?", *id)
}

func (s *server) newLog(userID string, item dbmodels.Item, optID *uint, date, today string, value float64, note string) dbmodels.CheckinLog {
	return dbmodels.CheckinLog{
		UserID: userID, ItemID: item.ID, CheckinDate: date, CheckinTime: s.now().UTC(),
		OptionID: optID, Value: value, Note: note, Source: dbmodels.SourceWeb, IsSupplement: date < today,
	}
}

// applyDelta appends a counter increment. The day's SUM(value) for the option
// must stay non-negative (the caller already holds the per-day lock on PG).
func (s *server) applyDelta(tx *gorm.DB, userID string, item dbmodels.Item, opt *dbmodels.ItemOption, date, today string, req checkinRequest) (result, error) {
	var optID *uint
	if opt != nil {
		optID = &opt.ID
	}
	var sum float64
	q := optionEq(tx.Model(&dbmodels.CheckinLog{}).
		Where("user_id = ? AND item_id = ? AND checkin_date = ?", userID, item.ID, date), optID)
	if err := q.Select("COALESCE(SUM(value), 0)").Scan(&sum).Error; err != nil {
		return result{}, err
	}
	total := sum + *req.Value
	if total < -1e-9 {
		return errResult(http.StatusBadRequest, "delta would make the day's total negative"), nil
	}
	row := s.newLog(userID, item, optID, date, today, *req.Value, req.Note)
	if err := tx.Create(&row).Error; err != nil {
		return result{}, err
	}
	return jsonResult(http.StatusOK, checkinResponse{
		ID: row.ID, ItemID: item.ID, Date: date, Mode: "delta", IsSupplement: row.IsSupplement,
		Note: row.Note, OptionID: optID, Value: &row.Value, Total: &total,
	}), nil
}

// applyAppend appends one measurement (metric) or one occurrence (negative
// boolean, value ignored).
func (s *server) applyAppend(tx *gorm.DB, userID string, item dbmodels.Item, opt *dbmodels.ItemOption, date, today string, req checkinRequest) (result, error) {
	var optID *uint
	if opt != nil {
		optID = &opt.ID
	}
	var value float64
	if item.TrackerType == dbmodels.TrackerMetric {
		value = *req.Value
	}
	row := s.newLog(userID, item, optID, date, today, value, req.Note)
	if err := tx.Create(&row).Error; err != nil {
		return result{}, err
	}
	resp := checkinResponse{
		ID: row.ID, ItemID: item.ID, Date: date, Mode: "append", IsSupplement: row.IsSupplement,
		Note: row.Note, OptionID: optID,
	}
	if item.TrackerType == dbmodels.TrackerMetric {
		resp.Value = &row.Value
	}
	return jsonResult(http.StatusOK, resp), nil
}
