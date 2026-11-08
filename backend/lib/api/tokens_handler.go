package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const (
	// apiTokenPrefix marks Daka API tokens (also used to refuse them outside the feed).
	apiTokenPrefix = "dk_"
	// maxTokenDays is the longest allowed token lifetime; defaultTokenDays applies when omitted.
	maxTokenDays     = 365
	defaultTokenDays = 90
	maxTokensPerUser = 50
)

// hashToken returns the stored form of a presented token: hex SHA-256. Tokens
// are 256-bit random values, so a fast unsalted hash is sufficient.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func newTokenValue() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return apiTokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

type tokenResponse struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	Scopes          []string   `json:"scopes"`
	Privacy         string     `json:"privacy"`
	IncludeNegative bool       `json:"include_negative"`
	ExpiresAt       time.Time  `json:"expires_at"`
	Expired         bool       `json:"expired"`
	RevokedAt       *time.Time `json:"revoked_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	// Token and FeedPath are only present in the creation response.
	Token    string `json:"token,omitempty"`
	FeedPath string `json:"feed_path,omitempty"`
}

func (s *server) newTokenResponse(t dbmodels.ApiToken) tokenResponse {
	return tokenResponse{
		ID: t.ID, Name: t.Name, Scopes: strings.Split(t.Scopes, ","), Privacy: string(t.Privacy),
		IncludeNegative: t.IncludeNegative, ExpiresAt: t.ExpiresAt.UTC(), Expired: !s.now().Before(t.ExpiresAt),
		RevokedAt: t.RevokedAt, LastUsedAt: t.LastUsedAt,
	}
}

type tokenRequest struct {
	Name            *string `json:"name"`
	Privacy         *string `json:"privacy"`
	ExpiresInDays   *int    `json:"expires_in_days"`
	IncludeNegative *bool   `json:"include_negative"`
	// Scope is "feed:read" (default) or "read". It is fixed at creation.
	Scope *string `json:"scope"`
}

// GET /api/tokens: token metadata only (never the token or its hash).
func (s *server) listTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var ts []dbmodels.ApiToken
	if err := s.db.Read().Where("user_id = ?", userID).Order("id").Find(&ts).Error; err != nil {
		internalError(w, err, "list tokens failed")
		return
	}
	out := make([]tokenResponse, len(ts))
	for i, t := range ts {
		out[i] = s.newTokenResponse(t)
	}
	jsonResult(http.StatusOK, map[string]any{"tokens": out}).write(w)
}

// POST /api/tokens (behind reauth): creates a feed:read (default) or read token. The plaintext
// is returned exactly once.
func (s *server) postToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req tokenRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	bad := func(msg string) { errResult(http.StatusBadRequest, msg).write(w) }
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		bad("name is required")
		return
	}
	name := strings.TrimSpace(*req.Name)
	if utf8.RuneCountInString(name) > maxNameLen {
		bad("name too long")
		return
	}
	scope := dbmodels.ScopeFeedRead
	if req.Scope != nil {
		switch sc := dbmodels.Scope(*req.Scope); sc {
		case dbmodels.ScopeFeedRead, dbmodels.ScopeRead:
			scope = sc
		default:
			bad("invalid scope")
			return
		}
	}
	if scope == dbmodels.ScopeRead && (req.IncludeNegative != nil || (req.Privacy != nil && *req.Privacy != string(dbmodels.PrivacyExcludeSensitive))) {
		// Read tokens always exclude sensitive items; there is no knob to loosen it.
		bad("privacy and include_negative apply to feed:read tokens only")
		return
	}
	privacy := dbmodels.PrivacyExcludeSensitive
	if req.Privacy != nil {
		switch p := dbmodels.Privacy(*req.Privacy); p {
		case dbmodels.PrivacyAll, dbmodels.PrivacyExcludeSensitive, dbmodels.PrivacyAnonymizeSensitive:
			privacy = p
		default:
			bad("invalid privacy")
			return
		}
	}
	days := defaultTokenDays
	if req.ExpiresInDays != nil {
		days = *req.ExpiresInDays
	}
	if days < 1 || days > maxTokenDays {
		bad("expires_in_days must be between 1 and 365")
		return
	}
	var n int64
	if err := s.db.Read().Model(&dbmodels.ApiToken{}).
		Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, s.now().UTC()).Count(&n).Error; err != nil {
		internalError(w, err, "count tokens failed")
		return
	}
	if n >= maxTokensPerUser {
		errResult(http.StatusConflict, "too many active tokens").write(w)
		return
	}
	plain, err := newTokenValue()
	if err != nil {
		internalError(w, err, "generate token failed")
		return
	}
	t := dbmodels.ApiToken{
		UserID: userID, TokenHash: hashToken(plain), Name: name, Scopes: string(scope),
		Privacy: privacy, ExpiresAt: s.now().Add(time.Duration(days) * 24 * time.Hour).UTC(),
	}
	if req.IncludeNegative != nil {
		t.IncludeNegative = *req.IncludeNegative
	}
	if err := s.db.Write().Create(&t).Error; err != nil {
		internalError(w, err, "create token failed")
		return
	}
	resp := s.newTokenResponse(t)
	resp.Token = plain
	if scope == dbmodels.ScopeFeedRead {
		resp.FeedPath = feedPath + "?token=" + plain
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResult(http.StatusCreated, resp).write(w)
}

// DELETE /api/tokens/{id} (behind reauth): revokes the token. The row is kept
// (revoked_at) so the listing shows what was revoked; repeating is idempotent.
func (s *server) deleteToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var t dbmodels.ApiToken
	err := s.db.Write().Where("id = ? AND user_id = ?", id, userID).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		errResult(http.StatusNotFound, "token not found").write(w)
		return
	}
	if err != nil {
		internalError(w, err, "load token failed")
		return
	}
	if t.RevokedAt == nil {
		now := s.now().UTC()
		if err := s.db.Write().Model(&dbmodels.ApiToken{}).Where("id = ?", t.ID).UpdateColumn("revoked_at", now).Error; err != nil {
			internalError(w, err, "revoke token failed")
			return
		}
	}
	jsonResult(http.StatusOK, map[string]any{"revoked": true}).write(w)
}
