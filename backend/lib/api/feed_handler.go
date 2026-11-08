package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/ratelimit"
	"github.com/elmhuangyu/daka/backend/lib/schedule"
)

const (
	feedPath = "/api/calendar/feed.ics"
	// The feed window (DESIGN 6.1): 30 days back to 180 days ahead of today.
	feedPastDays   = 30
	feedFutureDays = 180
	// feedEventMinutes is the length of a timed event (items with remind_time).
	feedEventMinutes = 30
	// anonymizedName replaces the names of sensitive items for
	// privacy=anonymize_sensitive tokens.
	anonymizedName = "[Private] Check-in"
	// lastUsedGranularity bounds how often last_used_at is written.
	lastUsedGranularity = time.Minute
)

// writeFeedUnauthorized writes the single, uniform failure body: it never reveals
// whether a token exists, is expired, revoked or lacks the scope.
func writeFeedUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="daka-feed"`)
	w.Header().Set("Cache-Control", "no-store")
	errResult(http.StatusUnauthorized, "unauthorized").write(w)
}

// presentedToken extracts the token: Authorization: Bearer wins over ?token=.
func presentedToken(r *http.Request) string {
	if tok := presentedBearer(r); tok != "" {
		return tok
	}
	return r.URL.Query().Get("token")
}

// authenticateFeed resolves the token. It returns (token, true) on success;
// otherwise it has already written the response.
func (s *server) authenticateFeed(w http.ResponseWriter, r *http.Request) (dbmodels.ApiToken, bool) {
	return s.authenticateToken(w, r, presentedToken(r), dbmodels.ScopeFeedRead)
}

// authenticateToken resolves tok, which must carry the given scope. Feed and
// read tokens share the failure limiters (per (IP, prefix) and per-IP aggregate)
// and the uniform 401, so a failure never reveals why it failed.
func (s *server) authenticateToken(w http.ResponseWriter, r *http.Request, tok string, scope dbmodels.Scope) (dbmodels.ApiToken, bool) {
	ip := s.clientIP.ClientIP(r)
	prefix := ratelimit.TokenPrefix(tok)
	// Two keys: (ip, token prefix) and an (ip, any) aggregate, so an attacker
	// cannot dodge the lockout by varying the prefix.
	if ok, wait := s.feedFail.Check(ip, prefix); !ok {
		ratelimit.WriteRateLimited(w, wait)
		return dbmodels.ApiToken{}, false
	}
	if ok, wait := s.feedIPFail.Check(ip, ""); !ok {
		ratelimit.WriteRateLimited(w, wait)
		return dbmodels.ApiToken{}, false
	}
	fail := func() (dbmodels.ApiToken, bool) {
		s.feedFail.Failure(ip, prefix)
		s.feedIPFail.Failure(ip, "")
		writeFeedUnauthorized(w)
		return dbmodels.ApiToken{}, false
	}
	if tok == "" || len(tok) > 256 {
		return fail()
	}
	hash := hashToken(tok)
	var t dbmodels.ApiToken
	err := s.db.Read().Where("token_hash = ?", hash).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fail()
	}
	if err != nil {
		internalError(w, err, "feed: load token failed")
		return dbmodels.ApiToken{}, false
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(t.TokenHash)) != 1 ||
		t.RevokedAt != nil || !s.now().Before(t.ExpiresAt) || !hasScope(t.Scopes, scope) {
		return fail()
	}
	s.feedFail.Success(ip, prefix)
	s.feedIPFail.Success(ip, "")
	return t, true
}

func hasScope(scopes string, want dbmodels.Scope) bool {
	for _, sc := range strings.Split(scopes, ",") {
		if dbmodels.Scope(strings.TrimSpace(sc)) == want {
			return true
		}
	}
	return false
}

// touchToken records last_used_at, at most once per lastUsedGranularity. The
// timestamp is informational, so a failed write is only logged.
func (s *server) touchToken(t dbmodels.ApiToken) {
	now := s.now().UTC()
	if t.LastUsedAt != nil && now.Sub(*t.LastUsedAt) < lastUsedGranularity {
		return
	}
	if err := s.db.Write().Model(&dbmodels.ApiToken{}).Where("id = ?", t.ID).UpdateColumn("last_used_at", now).Error; err != nil {
		log.Warn().Err(err).Uint("token_id", t.ID).Msg("feed: update last_used_at failed")
	}
}

// getFeed serves GET /api/calendar/feed.ics (DESIGN 6.1). It is mounted
// outside the session middleware and authenticates by feed:read token only.
// It is strictly read-only; any other method is refused before authentication.
func (s *server) getFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		errResult(http.StatusMethodNotAllowed, "method not allowed").write(w)
		return
	}
	// The token travels in the URL: keep it out of Referer headers and caches.
	w.Header().Set("Referrer-Policy", "no-referrer")
	t, ok := s.authenticateFeed(w, r)
	if !ok {
		return
	}
	body, err := s.buildFeed(t)
	if err != nil {
		internalError(w, err, "feed: build failed") // err carries no item names
		return
	}
	s.touchToken(t)
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=300")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	_, _ = w.Write(body)
}

// etagMatches implements the weak comparison of If-None-Match.
func etagMatches(header, etag string) bool {
	for _, c := range strings.Split(header, ",") {
		c = strings.TrimSpace(c)
		if c == "*" || strings.TrimPrefix(c, "W/") == etag {
			return true
		}
	}
	return false
}

// buildFeed renders the full snapshot for the token's user and privacy level.
// Anything not rendered (archived, deleted, sensitive-excluded, out of window)
// is thereby withdrawn from subscribers on their next refresh.
func (s *server) buildFeed(t dbmodels.ApiToken) ([]byte, error) {
	g := s.db.Read()
	st, err := userSettings(g, t.UserID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}
	today, err := schedule.Today(s.now(), st.Timezone)
	if err != nil {
		return nil, err
	}
	todayT, _ := time.Parse(dbmodels.DateLayout, today)
	start := todayT.AddDate(0, 0, -feedPastDays).Format(dbmodels.DateLayout)
	end := todayT.AddDate(0, 0, feedFutureDays).Format(dbmodels.DateLayout)
	cal := s.calendarFor(st)

	q := g.Where("user_id = ? AND archived = ?", t.UserID, false)
	if !t.IncludeNegative {
		q = q.Where("polarity <> ?", dbmodels.PolarityNegative)
	}
	var items []dbmodels.Item
	if err := q.Order("sort_order, id").Find(&items).Error; err != nil {
		return nil, err
	}
	sens, err := sensitiveFn(g, t.UserID)
	if err != nil {
		return nil, err
	}
	versions := map[uint][]dbmodels.ItemRuleVersion{}
	if len(items) > 0 {
		ids := make([]uint, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		var vs []dbmodels.ItemRuleVersion
		// Every version counts for SEQUENCE; due dates use those up to the window end.
		if err := g.Where("item_id IN ?", ids).Order("effective_from, id").Find(&vs).Error; err != nil {
			return nil, err
		}
		for _, v := range vs {
			versions[v.ItemID] = append(versions[v.ItemID], v)
		}
	}

	stamp := todayT.Format("20060102") + "T000000Z"
	w := &icsWriter{}
	w.line("BEGIN:VCALENDAR")
	w.line("VERSION:2.0")
	w.line("PRODID:-//Daka//Check-in feed//EN")
	w.line("CALSCALE:GREGORIAN")
	w.line("METHOD:PUBLISH")
	w.line("X-WR-CALNAME:Daka")
	for _, it := range items {
		sensitive := sens(it)
		if sensitive && t.Privacy != dbmodels.PrivacyAll && t.Privacy != dbmodels.PrivacyAnonymizeSensitive {
			continue // exclude_sensitive and any unknown level: nothing of the item leaves the server
		}
		vs := versions[it.ID]
		// Non-fixed spans have no RRULE dates; see fixedOnly.
		var dueVs []dbmodels.ItemRuleVersion
		for _, v := range vs {
			if v.EffectiveFrom <= end {
				dueVs = append(dueVs, v)
			}
		}
		due, err := schedule.DueDatesWith(fixedOnly(dueVs), start, end, cal)
		if err != nil {
			log.Warn().Err(err).Uint("item_id", it.ID).Msg("feed: skipping item with unevaluable rule")
			continue
		}
		name, desc := it.Name, it.Description
		if sensitive && t.Privacy != dbmodels.PrivacyAll { // anonymize_sensitive
			name, desc = anonymizedName, ""
		}
		seq := max(0, len(vs)-1)
		for _, ds := range due {
			if err := writeEvent(w, it, ds, name, desc, seq, stamp, loc); err != nil {
				return nil, err
			}
		}
	}
	w.line("END:VCALENDAR")
	return []byte(w.String()), nil
}

// writeEvent renders one VEVENT. The UID is derived from item id and date only,
// so it survives renames and rule edits; SEQUENCE grows with rule versions.
func writeEvent(w *icsWriter, it dbmodels.Item, date, name, desc string, seq int, stamp string, loc *time.Location) error {
	d, err := time.Parse(dbmodels.DateLayout, date)
	if err != nil {
		return err
	}
	w.line("BEGIN:VEVENT")
	w.line(fmt.Sprintf("UID:daka-%d-%s@daka", it.ID, d.Format("20060102")))
	w.line("DTSTAMP:" + stamp)
	w.line(fmt.Sprintf("SEQUENCE:%d", seq))
	timed := false
	if it.RemindTime != nil {
		if hm, perr := time.Parse("15:04", *it.RemindTime); perr == nil {
			local := time.Date(d.Year(), d.Month(), d.Day(), hm.Hour(), hm.Minute(), 0, 0, loc)
			w.line("DTSTART:" + local.UTC().Format("20060102T150405Z"))
			w.line("DTEND:" + local.Add(feedEventMinutes*time.Minute).UTC().Format("20060102T150405Z"))
			timed = true
		}
	}
	if !timed {
		w.line("DTSTART;VALUE=DATE:" + d.Format("20060102"))
		w.line("DTEND;VALUE=DATE:" + d.AddDate(0, 0, 1).Format("20060102"))
		w.line("TRANSP:TRANSPARENT")
	}
	w.line("SUMMARY:" + icsText(name))
	if desc != "" {
		w.line("DESCRIPTION:" + icsText(desc))
	}
	if timed {
		w.line("BEGIN:VALARM")
		w.line("ACTION:DISPLAY")
		w.line("DESCRIPTION:" + icsText(name))
		w.line("TRIGGER:PT0S")
		w.line("END:VALARM")
	}
	w.line("END:VEVENT")
	return nil
}
