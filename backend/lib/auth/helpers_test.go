package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/config"
	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// clock is a settable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)} }
func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := db.Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "t.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, dbmodels.AutoMigrate(d.Write()))
	require.NoError(t, d.Write().Create(&dbmodels.User{ID: DefaultUserID, Name: "default_user"}).Error)
	return d.Write()
}

// call runs h and returns the recorded response.
func call(h http.Handler, method, target, body string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.1:1234"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
