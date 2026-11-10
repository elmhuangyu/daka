package dbmodels

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	claimFrom = "default_user"
	claimTo   = "oidc_target"
)

func seedClaim(t *testing.T, g *gorm.DB) {
	t.Helper()
	require.NoError(t, g.Create(&User{ID: claimFrom}).Error)
	require.NoError(t, g.Create(&User{ID: claimTo}).Error)
	require.NoError(t, g.Create(&UserSettings{UserID: claimFrom, Timezone: "Asia/Shanghai", WeekStart: 1}).Error)
	require.NoError(t, g.Create(&UserSettings{UserID: claimTo, Timezone: "UTC", WeekStart: 1}).Error)
	require.NoError(t, g.Create(&Category{UserID: claimFrom, Name: "c"}).Error)
	require.NoError(t, g.Create(&Project{UserID: claimFrom, Name: "p"}).Error)
	require.NoError(t, g.Create(&Item{UserID: claimFrom, Name: "i", TrackerType: "boolean"}).Error)
	require.NoError(t, g.Create(&CheckinLog{UserID: claimFrom, ItemID: 1, CheckinDate: "2026-11-07", CheckinTime: time.Now()}).Error)
	require.NoError(t, g.Create(&IdempotencyKey{UserID: claimFrom, Key: "k", StatusCode: 200}).Error)
	require.NoError(t, g.Create(&FreezePeriod{UserID: claimFrom, StartDate: "2026-01-01", EndDate: "2026-01-02"}).Error)
	require.NoError(t, g.Create(&ApiToken{UserID: claimFrom, TokenHash: "h", Name: "t", Scopes: "read", ExpiresAt: time.Now().Add(time.Hour)}).Error)
	require.NoError(t, g.Create(&Session{ID: "s1", UserID: claimFrom, AuthMethod: AuthMethodSecret, ExpiresAt: time.Now().Add(time.Hour)}).Error)
	require.NoError(t, g.Create(&Session{ID: "s2", UserID: claimTo, AuthMethod: AuthMethodOIDC, ExpiresAt: time.Now().Add(time.Hour)}).Error)
}

func count(t *testing.T, g *gorm.DB, m any, user string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, g.Model(m).Where("user_id = ?", user).Count(&n).Error)
	return n
}

func TestClaimUserMovesEverything(t *testing.T) {
	g := openTemp(t).Write()
	seedClaim(t, g)
	require.NoError(t, ClaimUser(g, claimFrom, claimTo))
	for _, m := range UserOwnedTables() {
		require.Zero(t, count(t, g, m, claimFrom), "%T still has rows for the source user", m)
	}
	for _, m := range []any{&Category{}, &Project{}, &Item{}, &CheckinLog{}, &IdempotencyKey{}, &FreezePeriod{}, &ApiToken{}} {
		require.EqualValues(t, 1, count(t, g, m, claimTo), "%T", m)
	}
	var st UserSettings
	require.NoError(t, g.First(&st, "user_id = ?", claimTo).Error)
	require.Equal(t, "Asia/Shanghai", st.Timezone, "settings follow the data")
	// the old default session must not become a login for the OIDC user
	require.EqualValues(t, 1, count(t, g, &Session{}, claimTo))
	var s Session
	require.NoError(t, g.First(&s, "user_id = ?", claimTo).Error)
	require.Equal(t, "s2", s.ID)
}

func TestClaimUserRefusals(t *testing.T) {
	g := openTemp(t).Write()
	seedClaim(t, g)
	require.ErrorContains(t, ClaimUser(g, claimFrom, claimFrom), "differ")
	require.ErrorContains(t, ClaimUser(g, "", claimTo), "required")
	require.ErrorContains(t, ClaimUser(g, claimFrom, "nobody"), "does not exist")

	require.NoError(t, g.Create(&Item{UserID: claimTo, Name: "mine", TrackerType: "boolean"}).Error)
	require.ErrorContains(t, ClaimUser(g, claimFrom, claimTo), "already has data")
	// nothing changed (transaction)
	require.EqualValues(t, 1, count(t, g, &Item{}, claimFrom))
	require.EqualValues(t, 1, count(t, g, &Category{}, claimFrom))
	require.EqualValues(t, 1, count(t, g, &Project{}, claimFrom))
	var st UserSettings
	require.NoError(t, g.First(&st, "user_id = ?", claimTo).Error)
	require.Equal(t, "UTC", st.Timezone)
}
