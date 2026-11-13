package dbmodels

// Date representation: every calendar date column (checkin_date,
// effective_from, dtstart, until) is stored as a zero-padded local
// "YYYY-MM-DD" string in a varchar(10) column. Strings round-trip identically
// on SQLite and PostgreSQL (no driver time-zone coercion) and sort
// lexicographically in calendar order, so <=, MAX and ORDER BY work as
// expected. Nullable dates are *string. Timestamps (created_at, checkin_time)
// are time.Time and are always stored in UTC.

// DateLayout is the Go layout of the stored date strings.
const DateLayout = "2006-01-02"

type TrackerType string

const (
	TrackerBoolean     TrackerType = "boolean"
	TrackerCounter     TrackerType = "counter"
	TrackerMaintenance TrackerType = "maintenance"
	TrackerMetric      TrackerType = "metric"
)

type Kind string

const (
	KindOngoing Kind = "ongoing"
	KindEvent   Kind = "event"
)

type Polarity string

const (
	PolarityPositive Polarity = "positive"
	PolarityNegative Polarity = "negative"
)

type Aggregate string

const (
	AggregateSum    Aggregate = "sum"
	AggregateLatest Aggregate = "latest"
	AggregateAvg    Aggregate = "avg"
)

type ScheduleType string

const (
	ScheduleFixedRRule ScheduleType = "fixed_rrule"
	ScheduleRolling    ScheduleType = "rolling"
	ScheduleQuota      ScheduleType = "quota"
	ScheduleNone       ScheduleType = "none"
)

type HolidayMode string

const (
	HolidayNone         HolidayMode = "none"
	HolidaySkipHolidays HolidayMode = "skip_holidays"
	HolidayWorkdaysOnly HolidayMode = "workdays_only"
)

type RollingUnit string

const (
	RollingDay   RollingUnit = "day"
	RollingMonth RollingUnit = "month"
	RollingYear  RollingUnit = "year"
)

type QuotaComparator string

const (
	QuotaAtLeast QuotaComparator = "at_least"
	QuotaAtMost  QuotaComparator = "at_most"
)

type QuotaPeriod string

const (
	QuotaWeek  QuotaPeriod = "week"
	QuotaMonth QuotaPeriod = "month"
	QuotaYear  QuotaPeriod = "year"
)

type QuotaWindow string

const (
	QuotaWindowCalendar QuotaWindow = "calendar"
	QuotaWindowRolling  QuotaWindow = "rolling"
)

type QuotaMeasure string

const (
	QuotaMeasureDays        QuotaMeasure = "days"
	QuotaMeasureOccurrences QuotaMeasure = "occurrences"
	QuotaMeasureSum         QuotaMeasure = "sum"
)

type CompletionRule string

const (
	CompletionAny     CompletionRule = "any"
	CompletionAll     CompletionRule = "all"
	CompletionAtLeast CompletionRule = "at_least_n"
)

type CheckinSource string

const (
	SourceWeb      CheckinSource = "web"
	SourceAPI      CheckinSource = "api"
	SourceAI       CheckinSource = "ai"
	SourceTelegram CheckinSource = "telegram"
	SourceImport   CheckinSource = "import"
)

type HolidayRegion string

const (
	RegionCN HolidayRegion = "CN"
	RegionUS HolidayRegion = "US"
)

// Privacy is the sensitivity filter bound to an ApiToken (server-side only).
type Privacy string

const (
	PrivacyAll                Privacy = "all"
	PrivacyExcludeSensitive   Privacy = "exclude_sensitive"
	PrivacyAnonymizeSensitive Privacy = "anonymize_sensitive"
)

// Scope is an ApiToken permission; ApiToken.Scopes holds a comma-separated list.
type Scope string

const (
	ScopeFeedRead Scope = "feed:read"
	ScopeRead     Scope = "read"
)
