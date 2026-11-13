package api

import "github.com/elmhuangyu/daka/backend/lib/dbmodels"

// Export / import schema (DESIGN 8, P4-4). One versioned JSON document holds
// the user's own data. Ids are only meaningful inside the document: import
// remaps all of them. user_id never appears in the document, and import
// rejects unknown fields, so a crafted file cannot name another user.
const (
	exportSchemaName    = "daka.export"
	exportSchemaVersion = 1
)

type exportSettings struct {
	Timezone            string `json:"timezone"`
	WeekStart           int    `json:"week_start"`
	HolidayRegion       string `json:"holiday_region"`
	StreakHolidayFreeze bool   `json:"streak_holiday_freeze"`
	Language            string `json:"language"`
	// ExternalCalendars carries name and url_hint only: the (encrypted) feed
	// URLs are secrets and are never exported. Import ignores the field.
	ExternalCalendars []externalCalendarOut `json:"external_calendars"`
}

type exportCategory struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
	IsSensitive bool   `json:"is_sensitive"`
	SortOrder   int    `json:"sort_order"`
}

type exportProject struct {
	ID          uint    `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Color       string  `json:"color"`
	Emoji       string  `json:"emoji,omitempty"`
	Status      string  `json:"status"`
	TargetDate  *string `json:"target_date"`
	IsSensitive bool    `json:"is_sensitive"`
	SortOrder   int     `json:"sort_order"`
	CategoryID  *uint   `json:"category_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type exportOption struct {
	ID        uint     `json:"id"`
	Name      string   `json:"name"`
	Unit      string   `json:"unit"`
	Step      float64  `json:"step"`
	MinVal    *float64 `json:"min_val"`
	MaxVal    *float64 `json:"max_val"`
	Aggregate string   `json:"aggregate"`
	SortOrder int      `json:"sort_order"`
	Archived  bool     `json:"archived"`
}

// exportRule is one immutable rule version. The array order of an item's
// rule_versions is the creation order (ascending id) and is preserved on
// import, because ties on effective_from are broken by the highest id.
type exportRule struct {
	EffectiveFrom string  `json:"effective_from"`
	ScheduleType  string  `json:"schedule_type"`
	DtStart       string  `json:"dtstart"`
	Until         *string `json:"until"`
	RRuleString   string  `json:"rrule_string"`
	HolidayMode   string  `json:"holiday_mode"`

	RollingInterval int    `json:"rolling_interval"`
	RollingUnit     string `json:"rolling_unit"`

	QuotaComparator string  `json:"quota_comparator"`
	QuotaCount      float64 `json:"quota_count"`
	QuotaPeriod     string  `json:"quota_period"`
	QuotaWindow     string  `json:"quota_window"`
	QuotaMeasure    string  `json:"quota_measure"`
	QuotaOptionID   *uint   `json:"quota_option_id"`

	TargetValue    *float64           `json:"target_value"`
	CompletionRule string             `json:"completion_rule"`
	CompletionN    int                `json:"completion_n"`
	StaleAfterDays int                `json:"stale_after_days"`
	OptionTargets  map[string]float64 `json:"option_targets"`
}

type exportItem struct {
	ID          uint     `json:"id"`
	CategoryID  *uint    `json:"category_id"`
	ProjectID   *uint    `json:"project_id,omitempty"`
	ParentID    *uint    `json:"parent_id,omitempty"`
	Name        string   `json:"name"`
	Emoji       string   `json:"emoji,omitempty"`
	Description string   `json:"description"`
	TrackerType string   `json:"tracker_type"`
	Unit        string   `json:"unit"`
	Aggregate   string   `json:"aggregate"`
	Step        float64  `json:"step"`
	MinVal      *float64 `json:"min_val"`
	MaxVal      *float64 `json:"max_val"`
	WarningDays int      `json:"warning_days"`
	Kind        string   `json:"kind"`
	Polarity    string   `json:"polarity"`
	Pinned      bool     `json:"pinned"`
	IsSensitive bool     `json:"is_sensitive"`
	RemindTime  *string  `json:"remind_time"`
	SortOrder   int      `json:"sort_order"`
	Archived    bool     `json:"archived"`

	Options      []exportOption `json:"options"`
	RuleVersions []exportRule   `json:"rule_versions"`
}

type exportCheckin struct {
	ID           uint    `json:"id"`
	ItemID       uint    `json:"item_id"`
	OptionID     *uint   `json:"option_id"`
	CheckinDate  string  `json:"checkin_date"`
	CheckinTime  string  `json:"checkin_time"` // RFC 3339, UTC
	Value        float64 `json:"value"`
	Note         string  `json:"note"`
	Source       string  `json:"source"`
	ExternalRef  *string `json:"external_ref"`
	IsSupplement bool    `json:"is_supplement"`
}

type exportFreeze struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Reason    string `json:"reason"`
}

// exportHeader is the document without its large arrays, which are streamed.
type exportHeader struct {
	Schema     string         `json:"schema"`
	Version    int            `json:"version"`
	ExportedAt string         `json:"exported_at"`
	Settings   exportSettings `json:"settings"`
}

// importDoc is the whole document as read by import.
type importDoc struct {
	Schema     string           `json:"schema"`
	Version    int              `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Settings   *exportSettings  `json:"settings"`
	Categories []exportCategory `json:"categories"`
	Projects   []exportProject  `json:"projects,omitempty"`
	Items      []exportItem     `json:"items"`
	Checkins   []exportCheckin  `json:"checkins"`
	Freezes    []exportFreeze   `json:"freezes"`
}

func ruleToExport(v dbmodels.ItemRuleVersion) exportRule {
	return exportRule{
		EffectiveFrom: v.EffectiveFrom, ScheduleType: string(v.ScheduleType), DtStart: v.DtStart, Until: v.Until,
		RRuleString: v.RRuleString, HolidayMode: string(v.HolidayMode),
		RollingInterval: v.RollingInterval, RollingUnit: string(v.RollingUnit),
		QuotaComparator: string(v.QuotaComparator), QuotaCount: v.QuotaCount, QuotaPeriod: string(v.QuotaPeriod),
		QuotaWindow: string(v.QuotaWindow), QuotaMeasure: string(v.QuotaMeasure), QuotaOptionID: v.QuotaOptionID,
		TargetValue: v.TargetValue, CompletionRule: string(v.CompletionRule), CompletionN: v.CompletionN,
		StaleAfterDays: v.StaleAfterDays, OptionTargets: optionTargetsOf(v),
	}
}
