package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/auth"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
	"github.com/elmhuangyu/daka/backend/lib/stats"
)

// errAbort rolls a transaction back after a business failure (a 4xx result).
var errAbort = errors.New("api: abort transaction")

// txResult runs fn in one write transaction. A result with a status >= 400
// rolls the transaction back, so handlers can validate after partial writes.
func (s *server) txResult(fn func(tx *gorm.DB) (result, error)) (result, error) {
	var out result
	err := s.db.Write().Transaction(func(tx *gorm.DB) error {
		r, err := fn(tx)
		out = r
		if err != nil {
			return err
		}
		if r.status >= 400 {
			return errAbort
		}
		return nil
	})
	if errors.Is(err, errAbort) {
		return out, nil
	}
	return out, err
}

// decodeStrict decodes one JSON document, rejecting unknown fields and
// trailing data. It writes the 400 itself and reports false on failure.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		errResult(http.StatusBadRequest, "invalid JSON body").write(w)
		return false
	}
	return true
}

// requestUser returns the authenticated user id or writes a 401.
func requestUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		errResult(http.StatusUnauthorized, "unauthorized").write(w)
		return "", false
	}
	return u.ID, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uint, bool) {
	id, err := strconv.ParseUint(r.PathValue(name), 10, 32)
	if err != nil || id == 0 {
		errResult(http.StatusBadRequest, "invalid id").write(w)
		return 0, false
	}
	return uint(id), true
}

func internalError(w http.ResponseWriter, err error, msg string) {
	log.Error().Err(err).Msg(msg)
	errResult(http.StatusInternalServerError, "internal error").write(w)
}

// itemStatsSummary is the compact stats attached by GET /api/items?include=stats.
type itemStatsSummary struct {
	LastAt        *string              `json:"last_at"`
	DaysSinceLast *int                 `json:"days_since_last"`
	Counts        stats.Counts         `json:"counts"`
	Quota         *stats.QuotaProgress `json:"quota"`
	// Maintenance is set for rolling items (Today's overdue / warning cards).
	Maintenance *stats.MaintenanceInfo `json:"maintenance"`
	Summary     string                 `json:"summary"`
}

// currentOrLatest is the rule in effect on date, or the newest version when
// every version starts later (an item created with a future retro-dated edit).
func currentOrLatest(vs []dbmodels.ItemRuleVersion, date string) (dbmodels.ItemRuleVersion, bool) {
	if cur := schedule.CurrentVersion(vs, date); cur != nil {
		return *cur, true
	}
	var best *dbmodels.ItemRuleVersion
	for i := range vs {
		v := &vs[i]
		if best == nil || v.EffectiveFrom > best.EffectiveFrom || (v.EffectiveFrom == best.EffectiveFrom && v.ID > best.ID) {
			best = v
		}
	}
	if best == nil {
		return dbmodels.ItemRuleVersion{}, false
	}
	return *best, true
}

func optionTargetsOf(v dbmodels.ItemRuleVersion) map[string]float64 {
	t := map[string]float64{}
	if v.OptionTargets != "" {
		_ = json.Unmarshal([]byte(v.OptionTargets), &t)
	}
	return t
}

func newOptionResponse(o dbmodels.ItemOption, targets map[string]float64) optionResponse {
	r := optionResponse{
		ID: o.ID, Name: o.Name, Unit: o.Unit, Step: o.Step, MinVal: o.MinVal, MaxVal: o.MaxVal,
		Aggregate: string(o.Aggregate), SortOrder: o.SortOrder, Archived: o.Archived,
	}
	if t, ok := targets[strconv.FormatUint(uint64(o.ID), 10)]; ok {
		r.TargetValue = &t
	}
	return r
}

// buildItemResponses renders items with their non-archived options and the
// rule in effect on today. Archived options are never part of an item payload;
// GET /api/items/{id}/options?include_archived=1 lists them.
func buildItemResponses(db *gorm.DB, items []dbmodels.Item, today string) ([]itemResponse, error) {
	out := make([]itemResponse, len(items))
	if len(items) == 0 {
		return out, nil
	}
	ids := make([]uint, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	var vers []dbmodels.ItemRuleVersion
	if err := db.Where("item_id IN ?", ids).Find(&vers).Error; err != nil {
		return nil, err
	}
	var opts []dbmodels.ItemOption
	if err := db.Where("item_id IN ? AND archived = ?", ids, false).Order("sort_order, id").Find(&opts).Error; err != nil {
		return nil, err
	}
	vsBy := map[uint][]dbmodels.ItemRuleVersion{}
	for _, v := range vers {
		vsBy[v.ItemID] = append(vsBy[v.ItemID], v)
	}
	optBy := map[uint][]dbmodels.ItemOption{}
	for _, o := range opts {
		optBy[o.ItemID] = append(optBy[o.ItemID], o)
	}
	sens, err := sensitiveFn(db, items[0].UserID)
	if err != nil {
		return nil, err
	}
	for i, it := range items {
		cur, _ := currentOrLatest(vsBy[it.ID], today)
		targets := optionTargetsOf(cur)
		resp := itemResponse{
			ID: it.ID, Name: it.Name, Description: it.Description, TrackerType: string(it.TrackerType),
			Kind: string(it.Kind), Polarity: string(it.Polarity), Color: it.Color, CategoryID: it.CategoryID,
			IsSensitive: it.IsSensitive, EffectiveSensitive: sens(it), Pinned: it.Pinned, SortOrder: it.SortOrder, Archived: it.Archived,
			RemindTime: it.RemindTime, Unit: it.Unit, Step: it.Step, MinVal: it.MinVal, MaxVal: it.MaxVal,
			Aggregate: string(it.Aggregate), WarningDays: it.WarningDays,
			Options:     make([]optionResponse, len(optBy[it.ID])),
			CurrentRule: newRuleResponse(cur),
		}
		for j, o := range optBy[it.ID] {
			resp.Options[j] = newOptionResponse(o, targets)
		}
		out[i] = resp
	}
	return out, nil
}

func redactItem(resp *itemResponse) {
	resp.Name = privateItemName
	resp.Description = ""
	resp.Unit = ""
	for i := range resp.Options {
		resp.Options[i].Name = "Option " + strconv.Itoa(i+1)
		resp.Options[i].Unit = ""
	}
}

// loadOwnedItem fetches one item of the user; ok is false when it does not exist.
func loadOwnedItem(db *gorm.DB, userID string, id uint) (dbmodels.Item, bool, error) {
	var it dbmodels.Item
	err := db.Where("id = ? AND user_id = ?", id, userID).Take(&it).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return it, false, nil
	}
	return it, err == nil, err
}

// escapeLike escapes LIKE wildcards; the query uses ESCAPE '\'.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

const (
	maxSearchTerms   = 8
	maxSearchRunes   = 200
	maxSearchTermLen = 64
)

// searchTerms splits a search query into lower-cased whitespace-separated
// terms; an item matches when its name contains every term (case-insensitive,
// any order). The count and length are bounded to cap the query cost.
func searchTerms(q string) ([]string, bool) {
	if utf8.RuneCountInString(q) > maxSearchRunes {
		return nil, false
	}
	terms := strings.Fields(strings.ToLower(q))
	if len(terms) > maxSearchTerms {
		return nil, false
	}
	for _, t := range terms {
		if utf8.RuneCountInString(t) > maxSearchTermLen {
			return nil, false
		}
	}
	return terms, true
}

// GET /api/items?q=&kind=&polarity=&category_id=&archived=&pinned=&include=stats&redact=1
func (s *server) listItems(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	db := s.db.Read()
	tx := db.Model(&dbmodels.Item{}).Where("user_id = ?", userID)
	bad := func(msg string) { errResult(http.StatusBadRequest, msg).write(w) }
	if v := q.Get("q"); v != "" {
		terms, ok := searchTerms(v)
		if !ok {
			bad("q has too many terms or is too long")
			return
		}
		for _, term := range terms {
			tx = tx.Where(`LOWER(name) LIKE ? ESCAPE '\'`, "%"+escapeLike(term)+"%")
		}
	}
	switch v := q.Get("kind"); v {
	case "":
	case string(dbmodels.KindHabit), string(dbmodels.KindEvent):
		tx = tx.Where("kind = ?", v)
	default:
		bad("invalid kind")
		return
	}
	switch v := q.Get("polarity"); v {
	case "":
	case string(dbmodels.PolarityPositive), string(dbmodels.PolarityNegative):
		tx = tx.Where("polarity = ?", v)
	default:
		bad("invalid polarity")
		return
	}
	if v := q.Get("category_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			bad("invalid category_id")
			return
		}
		tx = tx.Where("category_id = ?", id)
	}
	switch q.Get("archived") {
	case "", "false", "0":
		tx = tx.Where("archived = ?", false)
	case "true", "1":
		tx = tx.Where("archived = ?", true)
	case "all":
	default:
		bad("invalid archived")
		return
	}
	switch q.Get("pinned") {
	case "":
	case "true", "1":
		tx = tx.Where("pinned = ?", true)
	case "false", "0":
		tx = tx.Where("pinned = ?", false)
	default:
		bad("invalid pinned")
		return
	}
	withStats := false
	switch q.Get("include") {
	case "":
	case "stats":
		withStats = true
	default:
		bad("invalid include")
		return
	}

	var items []dbmodels.Item
	if err := tx.Order("pinned DESC, sort_order, id").Find(&items).Error; err != nil {
		internalError(w, err, "list items failed")
		return
	}
	st, err := userSettings(db, userID)
	if err != nil {
		internalError(w, err, "list items failed")
		return
	}
	today, err := schedule.Today(s.now(), st.Timezone)
	if err != nil {
		internalError(w, err, "list items failed")
		return
	}
	var sens func(dbmodels.Item) bool
	if isReadToken(r) {
		// Read tokens never see sensitive items (DESIGN 11.6): dropped, not masked.
		if sens, err = sensitiveFn(db, userID); err != nil {
			internalError(w, err, "list items failed")
			return
		}
		kept := items[:0]
		for _, it := range items {
			if !sens(it) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	resps, err := buildItemResponses(db, items, today)
	if err != nil {
		internalError(w, err, "list items failed")
		return
	}
	if redact && sens == nil {
		if sens, err = sensitiveFn(db, userID); err != nil {
			internalError(w, err, "list items failed")
			return
		}
	}
	for i := range resps {
		if withStats {
			val, err := s.cachedStats(db, userID, items[i])
			if err != nil {
				internalError(w, err, "list items stats failed")
				return
			}
			resps[i].Stats = &itemStatsSummary{
				LastAt: val.LastAt, DaysSinceLast: val.DaysSinceLast, Counts: val.Counts,
				Quota: val.Quota, Maintenance: val.Maintenance, Summary: val.Summary,
			}
		}
		if redact && sens(items[i]) {
			redactItem(&resps[i])
			if resps[i].Stats != nil {
				resps[i].Stats.Summary = ""
			}
		}
	}
	jsonResult(http.StatusOK, map[string]any{"items": resps}).write(w)
}

// GET /api/items/{id}
func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	redact, ok := parseRedact(w, r)
	if !ok {
		return
	}
	db := s.db.Read()
	item, found, err := loadOwnedItem(db, userID, id)
	if err != nil {
		internalError(w, err, "get item failed")
		return
	}
	if !found {
		errResult(http.StatusNotFound, "item not found").write(w)
		return
	}
	if isReadToken(r) {
		sens, err := sensitiveFn(db, userID)
		if err != nil {
			internalError(w, err, "get item failed")
			return
		}
		if sens(item) { // indistinguishable from a missing item
			errResult(http.StatusNotFound, "item not found").write(w)
			return
		}
	}
	resp, err := s.itemResponseOf(db, userID, item)
	if err != nil {
		internalError(w, err, "get item failed")
		return
	}
	if redact {
		sens, err := sensitiveFn(db, userID)
		if err != nil {
			internalError(w, err, "get item failed")
			return
		}
		if sens(item) {
			redactItem(&resp)
		}
	}
	jsonResult(http.StatusOK, resp).write(w)
}

func (s *server) itemResponseOf(db *gorm.DB, userID string, item dbmodels.Item) (itemResponse, error) {
	today, err := s.userToday(db, userID)
	if err != nil {
		return itemResponse{}, err
	}
	rs, err := buildItemResponses(db, []dbmodels.Item{item}, today)
	if err != nil {
		return itemResponse{}, err
	}
	return rs[0], nil
}
