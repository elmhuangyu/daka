package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	exportItemBatch    = 500
	exportCheckinBatch = 2000
)

// GET /api/export (behind reauth): the user's complete data as one JSON
// document. The response is plaintext sensitive data, hence reauth and
// Cache-Control: no-store. Large arrays are written in id-ordered batches from
// one read transaction (a consistent snapshot) so memory stays bounded. External
// calendar URLs are never exported (only name and url_hint).
func (s *server) getExport(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	now := s.now().UTC()
	var bw *bufio.Writer
	started := false
	err := s.db.Read().Transaction(func(tx *gorm.DB) error {
		st, err := s.loadSettings(tx, userID)
		if err != nil {
			return err
		}
		cs, err := s.calendarSettingsOf(st)
		if err != nil {
			return err
		}
		head, err := json.Marshal(exportHeader{
			Schema: exportSchemaName, Version: exportSchemaVersion, ExportedAt: now.Format(time.RFC3339),
			Settings: exportSettings(cs),
		})
		if err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Disposition", `attachment; filename="daka-export-`+now.Format("2006-01-02")+`.json"`)
		h.Set("X-Content-Type-Options", "nosniff")
		started = true
		bw = bufio.NewWriterSize(w, 32<<10)
		// The header object is closed; reopen it by dropping its final brace.
		if _, err := bw.Write(head[:len(head)-1]); err != nil {
			return err
		}
		if err := writeArray(bw, "categories", func(emit func(any) error) error { return exportCategories(tx, userID, emit) }); err != nil {
			return err
		}
		if err := writeArray(bw, "items", func(emit func(any) error) error { return exportItems(tx, userID, emit) }); err != nil {
			return err
		}
		if err := writeArray(bw, "checkins", func(emit func(any) error) error { return exportCheckins(tx, userID, emit) }); err != nil {
			return err
		}
		if err := writeArray(bw, "freezes", func(emit func(any) error) error { return exportFreezes(tx, userID, emit) }); err != nil {
			return err
		}
		if _, err := bw.WriteString("}\n"); err != nil {
			return err
		}
		return bw.Flush()
	})
	if err != nil {
		if !started {
			internalError(w, err, "export failed")
			return
		}
		// Part of the body is already sent: abort the connection so the client
		// sees a failed download instead of a truncated "successful" file.
		log.Error().Err(err).Msg("export failed mid-stream")
		panic(http.ErrAbortHandler)
	}
}

// writeArray appends `,"name":[...]` to the open top-level object.
func writeArray(bw *bufio.Writer, name string, fill func(emit func(any) error) error) error {
	key, _ := json.Marshal(name)
	if _, err := bw.WriteString(","); err != nil {
		return err
	}
	if _, err := bw.Write(key); err != nil {
		return err
	}
	if _, err := bw.WriteString(":["); err != nil {
		return err
	}
	first := true
	emit := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if !first {
			if _, err := bw.WriteString(","); err != nil {
				return err
			}
		}
		first = false
		_, err = bw.Write(b)
		return err
	}
	if err := fill(emit); err != nil {
		return err
	}
	_, err := bw.WriteString("]")
	return err
}

func exportCategories(tx *gorm.DB, userID string, emit func(any) error) error {
	var cats []dbmodels.Category
	if err := tx.Where("user_id = ?", userID).Order("id").Find(&cats).Error; err != nil {
		return err
	}
	for _, c := range cats {
		if err := emit(exportCategory{ID: c.ID, Name: c.Name, Color: c.Color, Icon: c.Icon, IsSensitive: c.IsSensitive, SortOrder: c.SortOrder}); err != nil {
			return err
		}
	}
	return nil
}

func exportItems(tx *gorm.DB, userID string, emit func(any) error) error {
	var after uint
	for {
		var items []dbmodels.Item
		if err := tx.Where("user_id = ? AND id > ?", userID, after).Order("id").Limit(exportItemBatch).Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		ids := make([]uint, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		var opts []dbmodels.ItemOption
		if err := tx.Where("item_id IN ?", ids).Order("id").Find(&opts).Error; err != nil {
			return err
		}
		var vers []dbmodels.ItemRuleVersion
		if err := tx.Where("item_id IN ?", ids).Order("id").Find(&vers).Error; err != nil {
			return err
		}
		optsBy := map[uint][]exportOption{}
		for _, o := range opts {
			optsBy[o.ItemID] = append(optsBy[o.ItemID], exportOption{
				ID: o.ID, Name: o.Name, Unit: o.Unit, Step: o.Step, MinVal: o.MinVal, MaxVal: o.MaxVal,
				Aggregate: string(o.Aggregate), SortOrder: o.SortOrder, Archived: o.Archived,
			})
		}
		versBy := map[uint][]exportRule{}
		for _, v := range vers {
			versBy[v.ItemID] = append(versBy[v.ItemID], ruleToExport(v))
		}
		for _, it := range items {
			e := exportItem{
				ID: it.ID, CategoryID: it.CategoryID, Name: it.Name, Description: it.Description,
				TrackerType: string(it.TrackerType), Unit: it.Unit, Aggregate: string(it.Aggregate), Step: it.Step,
				MinVal: it.MinVal, MaxVal: it.MaxVal, WarningDays: it.WarningDays, Kind: string(it.Kind),
				Polarity: string(it.Polarity), Pinned: it.Pinned, IsSensitive: it.IsSensitive, RemindTime: it.RemindTime,
				SortOrder: it.SortOrder, Archived: it.Archived,
				Options: optsBy[it.ID], RuleVersions: versBy[it.ID],
			}
			if e.Options == nil {
				e.Options = []exportOption{}
			}
			if e.RuleVersions == nil {
				e.RuleVersions = []exportRule{}
			}
			if err := emit(e); err != nil {
				return err
			}
		}
		after = items[len(items)-1].ID
	}
}

func exportCheckins(tx *gorm.DB, userID string, emit func(any) error) error {
	var after uint
	for {
		var logs []dbmodels.CheckinLog
		// Logs orphaned by a deleted item (see G25) are not exported; import
		// would reject them.
		owned := tx.Model(&dbmodels.Item{}).Select("id").Where("user_id = ?", userID)
		if err := tx.Where("user_id = ? AND id > ? AND item_id IN (?)", userID, after, owned).Order("id").Limit(exportCheckinBatch).Find(&logs).Error; err != nil {
			return err
		}
		if len(logs) == 0 {
			return nil
		}
		for _, l := range logs {
			if err := emit(exportCheckin{
				ID: l.ID, ItemID: l.ItemID, OptionID: l.OptionID, CheckinDate: l.CheckinDate,
				CheckinTime: l.CheckinTime.UTC().Format(time.RFC3339Nano), Value: l.Value, Note: l.Note,
				Source: string(l.Source), ExternalRef: l.ExternalRef, IsSupplement: l.IsSupplement,
			}); err != nil {
				return err
			}
		}
		after = logs[len(logs)-1].ID
	}
}

func exportFreezes(tx *gorm.DB, userID string, emit func(any) error) error {
	var fs []dbmodels.FreezePeriod
	if err := tx.Where("user_id = ?", userID).Order("id").Find(&fs).Error; err != nil {
		return err
	}
	for _, f := range fs {
		if err := emit(exportFreeze{StartDate: f.StartDate, EndDate: f.EndDate, Reason: f.Reason}); err != nil {
			return err
		}
	}
	return nil
}
