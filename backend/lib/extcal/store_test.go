package extcal

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

const (
	keyOne = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	keyTwo = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func newStore(t *testing.T, allowHTTP bool) *Store {
	t.Helper()
	k, err := secret.NewKeyring(keyOne)
	require.NoError(t, err)
	return NewStore(k, allowHTTP)
}

func sp(s string) *string { return &s }

func TestApplyEncryptsAndHidesURL(t *testing.T) {
	s := newStore(t, false)
	col, err := s.Apply("u1", "", []Input{{ID: "work", Name: "Work", URL: sp("https://cal.example.com/feeds/abc123/basic.ics?key=TOPSECRET")}})
	require.NoError(t, err)
	require.NotContains(t, col, "example.com")
	require.NotContains(t, col, "TOPSECRET")
	require.NotContains(t, col, "basic.ics")

	var es []Entry
	require.NoError(t, json.Unmarshal([]byte(col), &es))
	require.Len(t, es, 1)
	require.Equal(t, "work", es[0].ID)
	u, err := s.URL("u1", es[0])
	require.NoError(t, err)
	require.Equal(t, "https://cal.example.com/feeds/abc123/basic.ics?key=TOPSECRET", u)
	// Bound to the user and source id by AAD.
	_, err = s.URL("u2", es[0])
	require.ErrorIs(t, err, secret.ErrDecrypt)
	moved := es[0]
	moved.ID = "other"
	_, err = s.URL("u1", moved)
	require.ErrorIs(t, err, secret.ErrDecrypt)

	views, err := s.Views("u1", col)
	require.NoError(t, err)
	require.Equal(t, []View{{ID: "work", Name: "Work", URLHint: "https://cal.example.com/…"}}, views)
	b, _ := json.Marshal(views)
	require.NotContains(t, string(b), "TOPSECRET")
	require.NotContains(t, string(b), "abc123")
}

func TestHint(t *testing.T) {
	require.Equal(t, "https://h.example/…", Hint("https://h.example/"))
	require.Equal(t, "https://h.example/…", Hint("https://h.example?key=SECRET"))
	require.Equal(t, "https://h.example:8443/…", Hint("https://user:pw@h.example:8443/a/secret?token=SECRET"))
	require.Equal(t, "", Hint("::bad"))
}

func TestApplyKeepsCiphertextWhenURLOmittedOrMasked(t *testing.T) {
	s := newStore(t, false)
	col, err := s.Apply("u1", "", []Input{{ID: "a", Name: "A", URL: sp("https://cal.example.com/x/feed.ics?key=K1")}})
	require.NoError(t, err)
	before, _ := Entries(col)

	// B11: url omitted -> unchanged ciphertext (name may change).
	col2, err := s.Apply("u1", col, []Input{{ID: "a", Name: "Renamed"}})
	require.NoError(t, err)
	after, _ := Entries(col2)
	require.Equal(t, before[0].Enc, after[0].Enc)
	require.Equal(t, "Renamed", after[0].Name)

	// The masked hint echoed back is refused and cannot overwrite anything.
	hint := Hint("https://cal.example.com/x/feed.ics?key=K1")
	for _, bad := range []string{hint, "https://cal.example.com/…", "https://cal.example.com/***", "https://cal.example.com/ab...", "", "   "} {
		_, err = s.Apply("u1", col, []Input{{ID: "a", Name: "A", URL: sp(bad)}})
		var ie InputError
		require.ErrorAs(t, err, &ie, bad)
	}

	// A real new URL replaces it.
	col3, err := s.Apply("u1", col, []Input{{ID: "a", Name: "A", URL: sp("https://cal.example.com/y/other.ics")}})
	require.NoError(t, err)
	e3, _ := Entries(col3)
	require.NotEqual(t, before[0].Enc, e3[0].Enc)
	u, _ := s.URL("u1", e3[0])
	require.Equal(t, "https://cal.example.com/y/other.ics", u)

	// Omitting a source removes it; an empty list clears the column.
	col4, err := s.Apply("u1", col, nil)
	require.NoError(t, err)
	require.Empty(t, col4)
}

func TestApplyWorksWithoutKeyOnlyWhenNoEncryptionNeeded(t *testing.T) {
	withKey := newStore(t, false)
	col, err := withKey.Apply("u1", "", []Input{{ID: "a", Name: "A", URL: sp("https://cal.example.com/x.ics")}})
	require.NoError(t, err)

	noKey := NewStore(nil, false)
	_, err = noKey.Apply("u1", "", []Input{{Name: "A", URL: sp("https://cal.example.com/x.ics")}})
	require.ErrorIs(t, err, secret.ErrNoKey)
	// Renaming without touching the URL needs no key.
	col2, err := noKey.Apply("u1", col, []Input{{ID: "a", Name: "B"}})
	require.NoError(t, err)
	views, err := noKey.Views("u1", col2)
	require.NoError(t, err)
	require.Equal(t, []View{{ID: "a", Name: "B", URLHint: ""}}, views)
}

func TestApplyValidation(t *testing.T) {
	s := newStore(t, false)
	ok := func(id string) Input {
		return Input{ID: id, Name: "N", URL: sp("https://cal.example.com/" + id + ".ics")}
	}
	_, err := s.Apply("u1", "", []Input{ok("a"), ok("b"), ok("c")})
	require.NoError(t, err)
	_, err = s.Apply("u1", "", []Input{ok("a"), ok("b"), ok("c"), ok("d")})
	require.Error(t, err)
	_, err = s.Apply("u1", "", []Input{ok("a"), ok("a")})
	require.Error(t, err, "duplicate id")
	_, err = s.Apply("u1", "", []Input{{ID: "a", Name: "A"}})
	require.Error(t, err, "new source needs a url")
	_, err = s.Apply("u1", "", []Input{{ID: "bad id!", Name: "A", URL: sp("https://cal.example.com/")}})
	require.Error(t, err)
	_, err = s.Apply("u1", "", []Input{{Name: strings.Repeat("n", 101), URL: sp("https://cal.example.com/")}})
	require.Error(t, err)
	_, err = s.Apply("u1", "", []Input{{Name: strings.Repeat("好", 100), URL: sp("https://cal.example.com/")}})
	require.NoError(t, err, "100 runes are fine")
	_, err = s.Apply("u1", "", []Input{{Name: "  ", URL: sp("https://cal.example.com/")}})
	require.Error(t, err)

	for _, u := range []string{
		"http://cal.example.com/x.ics", "ftp://cal.example.com/x", "file:///etc/passwd", "javascript:alert(1)", "cal.example.com/x",
		"https://", "https://localhost/x", "https://127.0.0.1/x", "https://[::1]/x", "https://10.0.0.1/x", "https://169.254.169.254/latest",
		"https://[::ffff:10.0.0.1]/x", "https://cal.example.com/" + strings.Repeat("a", 2100), "https://cal.example.com/a b", "https://x.internal/x",
	} {
		_, err = s.Apply("u1", "", []Input{{Name: "N", URL: sp(u)}})
		require.Error(t, err, u)
		var ie InputError
		require.ErrorAs(t, err, &ie, u)
		require.NotContains(t, err.Error(), "cal.example.com")
	}
	// http is accepted only when allowed by config.
	_, err = newStore(t, true).Apply("u1", "", []Input{{Name: "N", URL: sp("http://cal.example.com/x.ics")}})
	require.NoError(t, err)
	// Credentials in the URL are allowed.
	_, err = s.Apply("u1", "", []Input{{Name: "N", URL: sp("https://me:pw@cal.example.com/x.ics")}})
	require.NoError(t, err)
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "t.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, dbmodels.AutoMigrate(d.Write()))
	return d.Write()
}

func TestReencryptAllRotatesKeys(t *testing.T) {
	g := openDB(t)
	oldRing, err := secret.NewKeyring(keyOne)
	require.NoError(t, err)
	oldStore := NewStore(oldRing, false)
	for _, uid := range []string{"u1", "u2", "u3"} {
		require.NoError(t, g.Create(&dbmodels.User{ID: uid}).Error)
		col := ""
		if uid != "u3" { // u3 has no calendars
			col, err = oldStore.Apply(uid, "", []Input{{ID: "a", Name: "A", URL: sp("https://cal.example.com/" + uid + "/feed.ics?key=RAWKEY")}})
			require.NoError(t, err)
		}
		require.NoError(t, g.Create(&dbmodels.UserSettings{UserID: uid, Timezone: "UTC", ExternalCalendarURLs: col}).Error)
	}

	newRing, err := secret.NewKeyring(keyTwo, keyOne)
	require.NoError(t, err)
	newStore := NewStore(newRing, false)
	n, err := newStore.ReencryptAll(g)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	var rows []dbmodels.UserSettings
	require.NoError(t, g.Order("user_id").Find(&rows).Error)
	for _, r := range rows[:2] {
		es, err := Entries(r.ExternalCalendarURLs)
		require.NoError(t, err)
		id, err := secret.KeyID(es[0].Enc)
		require.NoError(t, err)
		require.Equal(t, newRing.CurrentKeyID(), id)
		u, err := newStore.URL(r.UserID, es[0])
		require.NoError(t, err)
		require.Equal(t, "https://cal.example.com/"+r.UserID+"/feed.ics?key=RAWKEY", u)
		require.NotContains(t, r.ExternalCalendarURLs, "RAWKEY")
	}
	// Idempotent; the old key alone can no longer read the new ciphertexts.
	n, err = newStore.ReencryptAll(g)
	require.NoError(t, err)
	require.Zero(t, n)
	es, _ := Entries(rows[0].ExternalCalendarURLs)
	_, err = oldStore.URL("u1", es[0])
	require.ErrorIs(t, err, secret.ErrUnknownKey)
}

func serviceRig(t *testing.T, h http.Handler, mut func(*FetcherOptions)) (*Service, *rig, *Store) {
	t.Helper()
	r, f := newRig(t, h, mut)
	k, err := secret.NewKeyring(keyOne)
	require.NoError(t, err)
	return NewService(ServiceOptions{Keys: k, Fetcher: f}), r, NewStore(k, false)
}

func TestServiceSourcesAreIndependentAndCached(t *testing.T) {
	var hits atomic.Int32
	svc, r, store := serviceRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/slow":
			select {
			case <-time.After(3 * time.Second):
			case <-req.Context().Done():
			}
			return
		case "/good":
			hits.Add(1)
			_, _ = w.Write([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:<b>Hello</b>\r\nDTSTART;VALUE=DATE:20261110\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
		default:
			http.NotFound(w, req)
		}
	}), func(o *FetcherOptions) { o.Timeout = 200 * time.Millisecond })

	base := "https://example.com:" + r.port
	col, err := store.Apply("u1", "", []Input{
		{ID: "slow", Name: "Slow", URL: sp(base + "/slow?key=SECRETKEY")},
		{ID: "good", Name: "Good", URL: sp(base + "/good?key=SECRETKEY")},
		{ID: "blocked", Name: "Blocked", URL: sp("https://ten.test:" + r.port + "/x?key=SECRETKEY")},
	})
	require.NoError(t, err)

	opt := ParseOptions{From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}
	start := time.Now()
	res, err := svc.Load(context.Background(), "u1", col, "", opt)
	require.NoError(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
	require.Len(t, res, 3)
	require.Equal(t, "slow", res[0].ID)
	require.False(t, res[0].OK)
	require.Equal(t, "fetch_failed", res[0].Error)
	require.True(t, res[1].OK)
	require.Len(t, res[1].Events, 1)
	require.Equal(t, "<b>Hello</b>", res[1].Events[0].Title) // external text is returned as-is
	require.Equal(t, "blocked_address", res[2].Error)
	for _, sr := range res {
		require.NotContains(t, sr.Error, "SECRETKEY")
	}

	// Second call is served from the cache; single-source filter works.
	res, err = svc.Load(context.Background(), "u1", col, "good", opt)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.True(t, res[0].OK)
	require.EqualValues(t, 1, hits.Load())

	_, err = svc.Load(context.Background(), "u1", col, "nope", opt)
	require.ErrorIs(t, err, ErrUnknownSource)
}

func TestServiceNoKeyAndWrongUser(t *testing.T) {
	svc, _, store := serviceRig(t, http.HandlerFunc(ics), nil)
	col, err := store.Apply("u1", "", []Input{{ID: "a", Name: "A", URL: sp("https://example.com/x.ics")}})
	require.NoError(t, err)
	opt := ParseOptions{From: time.Now(), To: time.Now().Add(time.Hour)}
	res, err := svc.Load(context.Background(), "u2", col, "", opt)
	require.NoError(t, err)
	require.Equal(t, "decrypt_failed", res[0].Error)
	res, err = NewService(ServiceOptions{}).Load(context.Background(), "u1", col, "", opt)
	require.NoError(t, err)
	require.Equal(t, "no_key", res[0].Error)
}

func TestCacheTTLAndBounds(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newCache(func() time.Time { return now }, 15*time.Minute, 2, 100)
	k := func(b byte) [32]byte { return [32]byte{b} }
	c.put(k(1), []byte("one"))
	c.put(k(2), []byte("two"))
	c.put(k(3), []byte("three")) // evicts k(1)
	_, ok := c.get(k(1))
	require.False(t, ok)
	_, ok = c.get(k(3))
	require.True(t, ok)
	require.Equal(t, 2, c.len())

	now = now.Add(14 * time.Minute)
	_, ok = c.get(k(2))
	require.True(t, ok)
	now = now.Add(2 * time.Minute) // past 15 min TTL
	_, ok = c.get(k(2))
	require.False(t, ok)

	c.put(k(9), make([]byte, 101)) // larger than the byte budget: not cached
	_, ok = c.get(k(9))
	require.False(t, ok)
	c.put(k(5), make([]byte, 60))
	c.put(k(6), make([]byte, 60)) // byte budget evicts k(5)
	_, ok = c.get(k(5))
	require.False(t, ok)
}
