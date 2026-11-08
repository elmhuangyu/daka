package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	defaultCheckinLimit = 50
	maxCheckinLimit     = 200
)

type checkinLogResponse struct {
	ID           uint    `json:"id"`
	ItemID       uint    `json:"item_id"`
	OptionID     *uint   `json:"option_id"`
	Date         string  `json:"date"`
	Time         string  `json:"time"`
	Value        float64 `json:"value"`
	Note         string  `json:"note"`
	Source       string  `json:"source"`
	IsSupplement bool    `json:"is_supplement"`
}

// encodeCursor / decodeCursor: the cursor is the (date, id) of the last row of
// the previous page, base64url encoded. Rows are ordered newest first.
func encodeCursor(date string, id uint) string {
	return base64.RawURLEncoding.EncodeToString([]byte(date + "|" + strconv.FormatUint(uint64(id), 10)))
}

func decodeCursor(c string) (string, uint, bool) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", 0, false
	}
	date, idStr, ok := strings.Cut(string(b), "|")
	if !ok {
		return "", 0, false
	}
	if _, ok := strictDate(date); !ok {
		return "", 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return "", 0, false
	}
	return date, uint(id), true
}

// GET /api/checkins?item_id=&from=&to=&cursor=&limit=&redact=1: the single
// query entry for logs (DESIGN 11.3). Newest first, cursor pagination.
func (s *server) listCheckins(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bad := func(msg string) { errResult(http.StatusBadRequest, msg).write(w) }
	db := s.db.Read()
	tx := db.Model(&dbmodels.CheckinLog{}).Where("user_id = ?", userID)
	readTok := isReadToken(r)
	if readTok {
		// Logs of sensitive items are excluded in SQL so pagination stays exact.
		hiddenIDs, err := sensitiveItemIDs(db, userID)
		if err != nil {
			internalError(w, err, "list checkins failed")
			return
		}
		if len(hiddenIDs) > 0 {
			tx = tx.Where("item_id NOT IN ?", hiddenIDs)
		}
	}

	if v := q.Get("item_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			bad("invalid item_id")
			return
		}
		item, found, err := loadOwnedItem(db, userID, uint(id))
		if err != nil {
			internalError(w, err, "list checkins failed")
			return
		}
		if found && readTok {
			sens, err := sensitiveFn(db, userID)
			if err != nil {
				internalError(w, err, "list checkins failed")
				return
			}
			found = !sens(item)
		}
		if !found {
			errResult(http.StatusNotFound, "item not found").write(w)
			return
		}
		tx = tx.Where("item_id = ?", id)
	}
	from, to := q.Get("from"), q.Get("to")
	if from != "" {
		if _, ok := strictDate(from); !ok {
			bad("from must be YYYY-MM-DD")
			return
		}
		tx = tx.Where("checkin_date >= ?", from)
	}
	if to != "" {
		if _, ok := strictDate(to); !ok {
			bad("to must be YYYY-MM-DD")
			return
		}
		tx = tx.Where("checkin_date <= ?", to)
	}
	if from != "" && to != "" && from > to {
		bad("from must not be after to")
		return
	}
	limit := defaultCheckinLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxCheckinLimit {
			bad("limit must be between 1 and 200")
			return
		}
		limit = n
	}
	if c := q.Get("cursor"); c != "" {
		date, id, ok := decodeCursor(c)
		if !ok {
			bad("invalid cursor")
			return
		}
		tx = tx.Where("checkin_date < ? OR (checkin_date = ? AND id < ?)", date, date, id)
	}

	var rows []dbmodels.CheckinLog
	if err := tx.Order("checkin_date DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		internalError(w, err, "list checkins failed")
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		c := encodeCursor(rows[limit-1].CheckinDate, rows[limit-1].ID)
		next = &c
	}
	hidden := map[uint]bool{}
	if redact && len(rows) > 0 {
		sens, err := sensitiveFn(db, userID)
		if err != nil {
			internalError(w, err, "list checkins failed")
			return
		}
		ids := make([]uint, 0, len(rows))
		for _, l := range rows {
			ids = append(ids, l.ItemID)
		}
		var items []dbmodels.Item
		if err := db.Where("user_id = ? AND id IN ?", userID, ids).Find(&items).Error; err != nil {
			internalError(w, err, "list checkins failed")
			return
		}
		for _, it := range items {
			hidden[it.ID] = sens(it)
		}
	}
	out := make([]checkinLogResponse, len(rows))
	for i, l := range rows {
		out[i] = checkinLogResponse{
			ID: l.ID, ItemID: l.ItemID, OptionID: l.OptionID, Date: l.CheckinDate,
			Time: l.CheckinTime.UTC().Format(time.RFC3339), Value: l.Value, Note: l.Note,
			Source: string(l.Source), IsSupplement: l.IsSupplement,
		}
		if hidden[l.ItemID] {
			out[i].Note = ""
		}
	}
	jsonResult(http.StatusOK, map[string]any{"checkins": out, "next_cursor": next}).write(w)
}
