package db

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

func TestLoggerDoesNotLeakParams(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).WithContext(context.Background())

	d, err := Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "l.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, d.Write().AutoMigrate(&row{}))

	// Force an error so the trace is logged.
	err = d.Write().WithContext(ctx).Exec("INSERT INTO nope (name) VALUES (?)", "SECRET-ITEM").Error
	require.Error(t, err)
	out := buf.String()
	require.Contains(t, out, "INSERT INTO nope")
	require.NotContains(t, out, "SECRET-ITEM")
}

func TestLoggerSlowAndNotFound(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).WithContext(context.Background())
	l := NewLogger(WithSlowThreshold(time.Hour))

	l.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)
	require.Empty(t, buf.String(), "fast query not logged")

	l = NewLogger(WithSlowThreshold(time.Millisecond), WithIgnoreNotFoundError())
	l.Trace(ctx, time.Now().Add(-time.Second), func() (string, int64) { return "SELECT 1", 1 }, nil)
	require.Contains(t, buf.String(), `"level":"warn"`)

	buf.Reset()
	l.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 0 }, gorm.ErrRecordNotFound)
	require.Empty(t, buf.String())

	l.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 0 }, errors.New("boom"))
	require.Contains(t, buf.String(), `"level":"error"`)
	require.Contains(t, buf.String(), "boom")
}
