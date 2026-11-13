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
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/holiday"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

// Import limits. The body cap bounds memory (the document is decoded whole);
// the counts bound database growth per request.
const (
	maxImportBytes        = 64 << 20
	maxImportCategories   = 500
	maxImportProjects     = 1000
	maxImportItems        = 5000
	maxImportVersions     = 1000 // per item
	maxImportCheckins     = 500000
	maxImportFreezes      = 5000
	maxImportRRuleLen     = 2000
	maxImportAbsValue     = 1e12
	importCheckinBatch    = 500
	importErrCodeTooLarge = "too_large"
)

// importSummary is the response of POST /api/import (also for dry runs).
type importSummary struct {
	DryRun       bool `json:"dry_run"`
	Categories   int  `json:"categories"`
	Projects     int  `json:"projects"`
	Items        int  `json:"items"`
	Options      int  `json:"options"`
	RuleVersions int  `json:"rule_versions"`
	Checkins     int  `json:"checkins"`
	Freezes      int  `json:"freezes"`
	Settings     bool `json:"settings_applied"`
}

// importFail is a validation failure. It carries only a stable code and a
// document location (indexes), never user-supplied text.
type importFail struct {
	status int
	msg    string
}

func (f *importFail) result() result { return errResult(f.status, f.msg) }

func bad(where, code string) *importFail {
	return &importFail{status: http.StatusBadRequest, msg: "invalid import: " + where + ": " + code}
}

func finiteBounded(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && math.Abs(f) <= maxImportAbsValue
}

func finitePtr(p *float64) bool { return p == nil || finiteBounded(*p) }

// POST /api/import[?dry_run=1][&apply_settings=1] (behind reauth). The body is
// a document produced by GET /api/export. Everything is validated before the
// first write and written in one transaction (all or nothing). Data is added
// to the current user with fresh ids; existing data is never modified and
// rule history is appended as given (versions stay immutable). Settings are
// only applied with apply_settings=1.
func (s *server) postImport(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	dryRun := q.Get("dry_run") == "1" || q.Get("dry_run") == "true"
	applySettings := q.Get("apply_settings") == "1" || q.Get("apply_settings") == "true"

	var doc importDoc
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(&doc)
	if err == nil {
		if _, terr := dec.Token(); terr != io.EOF {
			err = errors.New("trailing data")
		}
	}
	if err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			errResult(http.StatusRequestEntityTooLarge, "import file too large").write(w)
			return
		}
		errResult(http.StatusBadRequest, "invalid import: not a valid daka export document").write(w)
		return
	}
	plan, fail := validateImport(&doc, applySettings, s.now())
	if fail != nil {
		fail.result().write(w)
		return
	}
	sum := plan.summary
	sum.DryRun = dryRun
	if dryRun {
		jsonResult(http.StatusOK, sum).write(w)
		return
	}
	err = s.db.Write().Transaction(func(tx *gorm.DB) error { return plan.write(tx, userID) })
	if err != nil {
		internalError(w, err, "import failed")
		return
	}
	jsonResult(http.StatusOK, sum).write(w)
}

type importPlan struct {
	doc      *importDoc
	summary  importSummary
	settings *exportSettings
	now      time.Time
}

func validateImport(doc *importDoc, applySettings bool, now time.Time) (*importPlan, *importFail) {
	if doc.Schema != exportSchemaName {
		return nil, bad("schema", "unknown_schema")
	}
	if doc.Version != exportSchemaVersion {
		return nil, bad("version", "unsupported_version")
	}
	if len(doc.Categories) > maxImportCategories || len(doc.Projects) > maxImportProjects || len(doc.Items) > maxImportItems ||
		len(doc.Checkins) > maxImportCheckins || len(doc.Freezes) > maxImportFreezes {
		return nil, &importFail{status: http.StatusRequestEntityTooLarge, msg: "invalid import: " + importErrCodeTooLarge}
	}
	p := &importPlan{doc: doc, now: now.UTC()}
	if applySettings {
		if doc.Settings == nil {
			return nil, bad("settings", "missing")
		}
		if f := validateImportSettings(doc.Settings, now); f != nil {
			return nil, f
		}
		p.settings = doc.Settings
		p.summary.Settings = true
	}

	catIDs := map[uint]bool{}
	for i, c := range doc.Categories {
		where := "categories[" + strconv.Itoa(i) + "]"
		n := strings.TrimSpace(c.Name)
		switch {
		case c.ID == 0 || catIDs[c.ID]:
			return nil, bad(where, "bad_id")
		case n == "" || utf8.RuneCountInString(n) > maxNameLen:
			return nil, bad(where, "bad_name")
		case len(c.Color) > 32 || len(c.Icon) > 64:
			return nil, bad(where, "bad_field")
		}
		catIDs[c.ID] = true
	}
	p.summary.Categories = len(doc.Categories)

	projIDs := map[uint]bool{}
	for i, pr := range doc.Projects {
		where := "projects[" + strconv.Itoa(i) + "]"
		n := strings.TrimSpace(pr.Name)
		switch {
		case pr.ID == 0 || projIDs[pr.ID]:
			return nil, bad(where, "bad_id")
		case n == "" || utf8.RuneCountInString(n) > maxNameLen:
			return nil, bad(where, "bad_name")
		case utf8.RuneCountInString(pr.Description) > maxDescriptionLen:
			return nil, bad(where, "bad_description")
		case len(pr.Color) > 32 || !validEmoji(pr.Emoji):
			return nil, bad(where, "bad_field")
		case pr.CategoryID != nil && !catIDs[*pr.CategoryID]:
			return nil, bad(where, "unknown_category")
		}
		switch dbmodels.ProjectStatus(pr.Status) {
		case "", dbmodels.ProjectStatusActive, dbmodels.ProjectStatusCompleted, dbmodels.ProjectStatusArchived:
		default:
			return nil, bad(where, "bad_field")
		}
		if pr.TargetDate != nil && *pr.TargetDate != "" {
			if _, ok := strictDate(*pr.TargetDate); !ok {
				return nil, bad(where, "bad_date")
			}
		}
		if pr.CreatedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, pr.CreatedAt); err != nil {
				return nil, bad(where, "bad_time")
			}
		}
		if pr.UpdatedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, pr.UpdatedAt); err != nil {
				return nil, bad(where, "bad_time")
			}
		}
		projIDs[pr.ID] = true
	}
	p.summary.Projects = len(doc.Projects)

	itemIDs := make(map[uint]map[uint]bool, len(doc.Items)) // item id -> option ids
	for i := range doc.Items {
		it := &doc.Items[i]
		where := "items[" + strconv.Itoa(i) + "]"
		if it.ID == 0 || itemIDs[it.ID] != nil {
			return nil, bad(where, "bad_id")
		}
		if it.CategoryID != nil && !catIDs[*it.CategoryID] {
			return nil, bad(where, "unknown_category")
		}
		if it.ProjectID != nil && !projIDs[*it.ProjectID] {
			return nil, bad(where, "unknown_project")
		}
		if it.ParentID != nil && *it.ParentID == it.ID {
			return nil, bad(where, "bad_parent")
		}
		optIDs, f := validateImportItem(where, it)
		if f != nil {
			return nil, f
		}
		itemIDs[it.ID] = optIDs
		p.summary.Options += len(it.Options)
		p.summary.RuleVersions += len(it.RuleVersions)
	}
	p.summary.Items = len(doc.Items)

	// Sub-items: the parent must be another item in the document, and the
	// parent chain must not loop.
	parentOf := make(map[uint]*uint, len(doc.Items))
	for i := range doc.Items {
		parentOf[doc.Items[i].ID] = doc.Items[i].ParentID
	}
	for i := range doc.Items {
		where := "items[" + strconv.Itoa(i) + "]"
		cur := doc.Items[i].ParentID
		for steps := 0; cur != nil; steps++ {
			next, ok := parentOf[*cur]
			if !ok || steps > len(doc.Items) {
				return nil, bad(where, "bad_parent")
			}
			cur = next
		}
	}

	for i, c := range doc.Checkins {
		where := "checkins[" + strconv.Itoa(i) + "]"
		opts, ok := itemIDs[c.ItemID]
		if !ok {
			return nil, bad(where, "unknown_item")
		}
		if c.OptionID != nil && !opts[*c.OptionID] {
			return nil, bad(where, "unknown_option")
		}
		if _, ok := strictDate(c.CheckinDate); !ok {
			return nil, bad(where, "bad_date")
		}
		if _, err := time.Parse(time.RFC3339Nano, c.CheckinTime); err != nil {
			return nil, bad(where, "bad_time")
		}
		if !finiteBounded(c.Value) {
			return nil, bad(where, "bad_value")
		}
		if len(c.Note) > maxNoteBytes || (c.ExternalRef != nil && len(*c.ExternalRef) > 255) {
			return nil, bad(where, "bad_field")
		}
		switch dbmodels.CheckinSource(c.Source) {
		case dbmodels.SourceWeb, dbmodels.SourceAPI, dbmodels.SourceAI, dbmodels.SourceTelegram, dbmodels.SourceImport:
		default:
			return nil, bad(where, "bad_source")
		}
	}
	p.summary.Checkins = len(doc.Checkins)

	for i, f := range doc.Freezes {
		where := "freezes[" + strconv.Itoa(i) + "]"
		start, ok1 := strictDate(f.StartDate)
		end, ok2 := strictDate(f.EndDate)
		switch {
		case !ok1 || !ok2:
			return nil, bad(where, "bad_date")
		case end.Before(start) || end.Sub(start) > maxFreezeDays*24*time.Hour:
			return nil, bad(where, "bad_range")
		case utf8.RuneCountInString(f.Reason) > maxFreezeReason:
			return nil, bad(where, "bad_field")
		}
	}
	p.summary.Freezes = len(doc.Freezes)
	return p, nil
}

func validateImportSettings(st *exportSettings, now time.Time) *importFail {
	if _, err := schedule.Today(now, st.Timezone); err != nil || st.Timezone == "" {
		return bad("settings", "bad_timezone")
	}
	if st.WeekStart != 0 && st.WeekStart != 1 {
		return bad("settings", "bad_week_start")
	}
	if st.HolidayRegion != "" && !holiday.ValidRegion(st.HolidayRegion) {
		return bad("settings", "bad_holiday_region")
	}
	if st.Language != "" && !validLanguage(st.Language) {
		return bad("settings", "bad_language")
	}
	return nil
}

// validateImportItem checks one item with the same validators as the item
// API and returns the item's option ids.
func validateImportItem(where string, it *exportItem) (map[uint]bool, *importFail) {
	name := strings.TrimSpace(it.Name)
	switch {
	case name == "" || utf8.RuneCountInString(name) > maxNameLen:
		return nil, bad(where, "bad_name")
	case utf8.RuneCountInString(it.Description) > maxDescriptionLen:
		return nil, bad(where, "bad_description")
	case it.RemindTime != nil && !remindTimeRe.MatchString(*it.RemindTime):
		return nil, bad(where, "bad_remind_time")
	case utf8.RuneCountInString(it.Unit) > 32 || !validEmoji(it.Emoji):
		return nil, bad(where, "bad_field")
	case !finitePtr(it.MinVal) || !finitePtr(it.MaxVal) || !finiteBounded(it.Step):
		return nil, bad(where, "bad_number")
	case len(it.Options) > maxOptions:
		return nil, bad(where, "too_many_options")
	case len(it.RuleVersions) == 0:
		return nil, bad(where, "no_rule_version")
	case len(it.RuleVersions) > maxImportVersions:
		return nil, &importFail{status: http.StatusRequestEntityTooLarge, msg: "invalid import: " + importErrCodeTooLarge}
	}
	spec := schedule.ItemSpec{
		TrackerType: dbmodels.TrackerType(it.TrackerType), Kind: dbmodels.Kind(it.Kind), Polarity: dbmodels.Polarity(it.Polarity),
		Unit: it.Unit, Step: it.Step, MinVal: it.MinVal, MaxVal: it.MaxVal,
		Aggregate: dbmodels.Aggregate(it.Aggregate), WarningDays: it.WarningDays,
	}
	optIDs := map[uint]bool{}
	var all, active []schedule.OptionSpec
	for j, o := range it.Options {
		ow := where + ".options[" + strconv.Itoa(j) + "]"
		n := strings.TrimSpace(o.Name)
		switch {
		case o.ID == 0 || optIDs[o.ID]:
			return nil, bad(ow, "bad_id")
		case n == "" || utf8.RuneCountInString(n) > maxNameLen:
			return nil, bad(ow, "bad_name")
		case utf8.RuneCountInString(o.Unit) > 32 || !finitePtr(o.MinVal) || !finitePtr(o.MaxVal) || !finiteBounded(o.Step):
			return nil, bad(ow, "bad_field")
		}
		optIDs[o.ID] = true
		sp := schedule.OptionSpec{ID: o.ID, Unit: o.Unit, Step: o.Step, MinVal: o.MinVal, MaxVal: o.MaxVal, Aggregate: dbmodels.Aggregate(o.Aggregate)}
		all = append(all, sp)
		if !o.Archived {
			active = append(active, sp)
		}
	}
	for j := range it.RuleVersions {
		rw := where + ".rule_versions[" + strconv.Itoa(j) + "]"
		rv := &it.RuleVersions[j]
		if f := validateImportRule(rw, spec, rv, optIDs, all, active); f != nil {
			return nil, f
		}
	}
	return optIDs, nil
}

func validateImportRule(where string, spec schedule.ItemSpec, rv *exportRule, optIDs map[uint]bool, all, active []schedule.OptionSpec) *importFail {
	if _, ok := strictDate(rv.EffectiveFrom); !ok {
		return bad(where, "bad_date")
	}
	if rv.DtStart != "" {
		if _, ok := strictDate(rv.DtStart); !ok {
			return bad(where, "bad_date")
		}
	}
	if rv.Until != nil && *rv.Until != "" {
		if _, ok := strictDate(*rv.Until); !ok {
			return bad(where, "bad_date")
		}
	}
	if len(rv.RRuleString) > maxImportRRuleLen {
		return bad(where, "bad_field")
	}
	if rv.QuotaOptionID != nil && !optIDs[*rv.QuotaOptionID] {
		return bad(where, "unknown_option")
	}
	// Option targets are keyed by option id, canonical decimal, and must refer
	// to this item's options.
	referenced := map[uint]bool{}
	if rv.QuotaOptionID != nil {
		referenced[*rv.QuotaOptionID] = true
	}
	for k, t := range rv.OptionTargets {
		id, err := strconv.ParseUint(k, 10, 32)
		if err != nil || strconv.FormatUint(id, 10) != k || !optIDs[uint(id)] {
			return bad(where, "bad_option_targets")
		}
		if !finiteBounded(t) {
			return bad(where, "bad_number")
		}
		referenced[uint(id)] = true
	}
	if !finiteBounded(rv.QuotaCount) || !finitePtr(rv.TargetValue) {
		return bad(where, "bad_number")
	}
	var ref []schedule.OptionSpec
	for _, o := range all {
		if referenced[o.ID] {
			ref = append(ref, o)
		}
	}
	ver := dbmodels.ItemRuleVersion{
		EffectiveFrom: rv.EffectiveFrom, ScheduleType: dbmodels.ScheduleType(rv.ScheduleType), DtStart: rv.DtStart,
		Until: rv.Until, RRuleString: rv.RRuleString, HolidayMode: dbmodels.HolidayMode(rv.HolidayMode),
		RollingInterval: rv.RollingInterval, RollingUnit: dbmodels.RollingUnit(rv.RollingUnit),
		QuotaComparator: dbmodels.QuotaComparator(rv.QuotaComparator), QuotaCount: rv.QuotaCount,
		QuotaPeriod: dbmodels.QuotaPeriod(rv.QuotaPeriod), QuotaWindow: dbmodels.QuotaWindow(rv.QuotaWindow),
		QuotaMeasure: dbmodels.QuotaMeasure(rv.QuotaMeasure), QuotaOptionID: rv.QuotaOptionID,
		TargetValue: rv.TargetValue, CompletionRule: dbmodels.CompletionRule(rv.CompletionRule),
		CompletionN: rv.CompletionN, StaleAfterDays: rv.StaleAfterDays, OptionTargets: marshalTargets(rv.OptionTargets),
	}
	// A historic version was valid against the options of its time, which may
	// since have been archived or added. Accept it when it is valid against any
	// plausible option set; every enum, range and combination check still runs.
	var firstErr error
	for _, opts := range [][]schedule.OptionSpec{active, all, ref, nil} {
		err := schedule.ValidateItemRule(spec, ver, opts)
		if err == nil {
			return nil
		}
		if !errors.Is(err, schedule.ErrInvalid) {
			return &importFail{status: http.StatusInternalServerError, msg: "internal error"}
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return bad(where, schedule.Reason(firstErr))
}

// write inserts the validated document for userID. It runs in the caller's
// transaction; any error rolls everything back.
func (p *importPlan) write(tx *gorm.DB, userID string) error {
	doc := p.doc
	catMap := make(map[uint]uint, len(doc.Categories))
	catSensitive := map[uint]bool{}
	for _, c := range doc.Categories {
		row := dbmodels.Category{UserID: userID, Name: strings.TrimSpace(c.Name), Color: c.Color, Icon: c.Icon, IsSensitive: c.IsSensitive, SortOrder: c.SortOrder}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		catMap[c.ID] = row.ID
		catSensitive[c.ID] = c.IsSensitive
	}

	projMap := make(map[uint]uint, len(doc.Projects))
	for _, pr := range doc.Projects {
		created := p.now
		if pr.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, pr.CreatedAt); err == nil {
				created = t.UTC()
			}
		}
		updated := p.now
		if pr.UpdatedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, pr.UpdatedAt); err == nil {
				updated = t.UTC()
			}
		}
		status := dbmodels.ProjectStatus(pr.Status)
		if status == "" {
			status = dbmodels.ProjectStatusActive
		}
		row := dbmodels.Project{
			UserID:      userID,
			Name:        strings.TrimSpace(pr.Name),
			Description: pr.Description,
			Color:       pr.Color,
			Emoji:       pr.Emoji,
			Status:      status,
			TargetDate:  pr.TargetDate,
			IsSensitive: pr.IsSensitive,
			SortOrder:   pr.SortOrder,
			CreatedAt:   created,
			UpdatedAt:   updated,
		}
		if pr.CategoryID != nil {
			nid := catMap[*pr.CategoryID]
			row.CategoryID = &nid
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		projMap[pr.ID] = row.ID
	}

	itemMap := make(map[uint]uint, len(doc.Items))
	// Option ids are only unique per item in a document, so key on both.
	type key struct{ item, opt uint }
	optKey := map[key]uint{}
	for i := range doc.Items {
		it := &doc.Items[i]
		row := dbmodels.Item{
			UserID: userID, Name: strings.TrimSpace(it.Name), Emoji: it.Emoji, Description: it.Description,
			TrackerType: dbmodels.TrackerType(it.TrackerType), Unit: it.Unit, Aggregate: dbmodels.Aggregate(it.Aggregate),
			Step: it.Step, MinVal: it.MinVal, MaxVal: it.MaxVal, WarningDays: it.WarningDays,
			Kind: dbmodels.Kind(it.Kind), Polarity: dbmodels.Polarity(it.Polarity), Pinned: it.Pinned,
			IsSensitive: it.IsSensitive, RemindTime: it.RemindTime, SortOrder: it.SortOrder, Archived: it.Archived,
		}
		if it.CategoryID != nil {
			nid := catMap[*it.CategoryID]
			row.CategoryID = &nid
			row.IsSensitive = row.IsSensitive || catSensitive[*it.CategoryID] // DESIGN 4.1 rule 1
		}
		if it.ProjectID != nil {
			nid := projMap[*it.ProjectID]
			row.ProjectID = &nid
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		itemMap[it.ID] = row.ID
		for _, o := range it.Options {
			orow := dbmodels.ItemOption{
				ItemID: row.ID, Name: strings.TrimSpace(o.Name), Unit: o.Unit, Step: o.Step, MinVal: o.MinVal,
				MaxVal: o.MaxVal, Aggregate: dbmodels.Aggregate(o.Aggregate), SortOrder: o.SortOrder, Archived: o.Archived,
			}
			if err := tx.Create(&orow).Error; err != nil {
				return err
			}
			optKey[key{it.ID, o.ID}] = orow.ID
		}
		// Versions are inserted in document order: ids ascend with it, which
		// keeps the "highest id wins" tie-break on equal effective_from.
		for _, rv := range it.RuleVersions {
			targets := map[string]float64{}
			for k, t := range rv.OptionTargets {
				id, _ := strconv.ParseUint(k, 10, 32)
				targets[strconv.FormatUint(uint64(optKey[key{it.ID, uint(id)}]), 10)] = t
			}
			ver := dbmodels.ItemRuleVersion{
				ItemID: row.ID, EffectiveFrom: rv.EffectiveFrom, ScheduleType: dbmodels.ScheduleType(rv.ScheduleType),
				DtStart: rv.DtStart, Until: rv.Until, RRuleString: rv.RRuleString, HolidayMode: dbmodels.HolidayMode(rv.HolidayMode),
				RollingInterval: rv.RollingInterval, RollingUnit: dbmodels.RollingUnit(rv.RollingUnit),
				QuotaComparator: dbmodels.QuotaComparator(rv.QuotaComparator), QuotaCount: rv.QuotaCount,
				QuotaPeriod: dbmodels.QuotaPeriod(rv.QuotaPeriod), QuotaWindow: dbmodels.QuotaWindow(rv.QuotaWindow),
				QuotaMeasure: dbmodels.QuotaMeasure(rv.QuotaMeasure), TargetValue: rv.TargetValue,
				CompletionRule: dbmodels.CompletionRule(rv.CompletionRule), CompletionN: rv.CompletionN,
				StaleAfterDays: rv.StaleAfterDays, OptionTargets: marshalTargets(targets),
			}
			if ver.Until != nil && *ver.Until == "" {
				ver.Until = nil
			}
			if rv.QuotaOptionID != nil {
				nid := optKey[key{it.ID, *rv.QuotaOptionID}]
				ver.QuotaOptionID = &nid
			}
			if err := tx.Create(&ver).Error; err != nil {
				return err
			}
		}
	}

	// Sub-item links are resolved after every item has its new id.
	for i := range doc.Items {
		it := &doc.Items[i]
		if it.ParentID == nil {
			continue
		}
		pid, ok := itemMap[*it.ParentID]
		if !ok {
			continue
		}
		if err := tx.Model(&dbmodels.Item{}).Where("id = ? AND user_id = ?", itemMap[it.ID], userID).Update("parent_id", pid).Error; err != nil {
			return err
		}
	}

	batch := make([]dbmodels.CheckinLog, 0, importCheckinBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := tx.Create(&batch).Error
		batch = batch[:0]
		return err
	}
	for _, c := range doc.Checkins {
		t, _ := time.Parse(time.RFC3339Nano, c.CheckinTime)
		l := dbmodels.CheckinLog{
			UserID: userID, ItemID: itemMap[c.ItemID], CheckinDate: c.CheckinDate, CheckinTime: t.UTC(),
			Value: c.Value, Note: c.Note, Source: dbmodels.CheckinSource(c.Source), ExternalRef: c.ExternalRef,
			IsSupplement: c.IsSupplement,
		}
		if c.OptionID != nil {
			nid := optKey[key{c.ItemID, *c.OptionID}]
			l.OptionID = &nid
		}
		batch = append(batch, l)
		if len(batch) == importCheckinBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	for _, f := range doc.Freezes {
		if err := tx.Create(&dbmodels.FreezePeriod{UserID: userID, StartDate: f.StartDate, EndDate: f.EndDate, Reason: f.Reason}).Error; err != nil {
			return err
		}
	}

	if p.settings != nil {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&dbmodels.UserSettings{UserID: userID, Timezone: "UTC", WeekStart: 1}).Error; err != nil {
			return err
		}
		if err := tx.Model(&dbmodels.UserSettings{}).Where("user_id = ?", userID).Updates(map[string]any{
			"timezone": p.settings.Timezone, "week_start": p.settings.WeekStart,
			"holiday_region": p.settings.HolidayRegion, "streak_holiday_freeze": p.settings.StreakHolidayFreeze,
			"language": p.settings.Language,
		}).Error; err != nil {
			return err
		}
	}
	// Any import changes derived values (streaks, freezes, quotas).
	if err := dbmodels.BumpStateVersion(tx, userID); err != nil {
		return fmt.Errorf("bump state version: %w", err)
	}
	return nil
}
