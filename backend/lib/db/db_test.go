package db

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

type row struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func openTemp(t *testing.T) *DB {
	t.Helper()
	d, err := Open(config.DBConfig{Driver: config.DriverSQLite, DSN: config.Secret(filepath.Join(t.TempDir(), "t.db"))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.NoError(t, d.Write().AutoMigrate(&row{}))
	return d
}

func TestSQLiteWALAndPools(t *testing.T) {
	d := openTemp(t)

	var mode string
	require.NoError(t, d.Write().Raw("PRAGMA journal_mode").Scan(&mode).Error)
	require.Equal(t, "wal", mode)

	var bt int
	require.NoError(t, d.Write().Raw("PRAGMA busy_timeout").Scan(&bt).Error)
	require.Equal(t, 10000, bt)

	w, err := d.Write().DB()
	require.NoError(t, err)
	require.Equal(t, 1, w.Stats().MaxOpenConnections)

	r, err := d.Read().DB()
	require.NoError(t, err)
	require.Greater(t, r.Stats().MaxOpenConnections, 1)
}

func TestReaderNotBlockedByWriterTx(t *testing.T) {
	d := openTemp(t)
	require.NoError(t, d.Write().Create(&row{Name: "before"}).Error)

	tx := d.Write().Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Create(&row{Name: "uncommitted"}).Error)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var n int64
	require.NoError(t, d.Read().WithContext(ctx).Model(&row{}).Count(&n).Error)
	require.EqualValues(t, 1, n)

	require.NoError(t, tx.Commit().Error)
	require.NoError(t, d.Read().Model(&row{}).Count(&n).Error)
	require.EqualValues(t, 2, n)
}

func TestReaderCannotWrite(t *testing.T) {
	d := openTemp(t)
	require.Error(t, d.Read().Create(&row{Name: "x"}).Error)
	require.Error(t, d.Read().Exec("DELETE FROM rows").Error)
}

func TestPoolConfig(t *testing.T) {
	sqlite := poolConfigFor(config.DriverSQLite, roleWrite)
	require.Equal(t, 1, sqlite.MaxOpenConns)
	rd := poolConfigFor(config.DriverSQLite, roleRead)
	require.Greater(t, rd.MaxOpenConns, 1)

	pg := poolConfigFor(config.DriverPostgres, roleWrite)
	require.Greater(t, pg.MaxOpenConns, 1)
	require.Equal(t, pg, poolConfigFor(config.DriverPostgres, roleRead))
}

func TestPostgresDSNHasNoSQLiteSettings(t *testing.T) {
	const dsn = "host=h user=u dbname=d"
	require.Equal(t, dsn, dialectDSN(config.DriverPostgres, dsn, roleWrite))
	require.Equal(t, dsn, dialectDSN(config.DriverPostgres, dsn, roleRead))
}

func TestSQLiteDSN(t *testing.T) {
	w, _ := url.QueryUnescape(dialectDSN(config.DriverSQLite, "/tmp/a b.db", roleWrite))
	require.Contains(t, w, "journal_mode(WAL)")
	require.Contains(t, w, "busy_timeout(10000)")
	require.NotContains(t, w, "mode=ro")
	r, _ := url.QueryUnescape(dialectDSN(config.DriverSQLite, "/tmp/a b.db", roleRead))
	require.Contains(t, r, "mode=ro")
	require.Contains(t, r, "query_only(1)")
	require.Contains(t, r, "busy_timeout(10000)")
}

func TestOpenUnknownDriver(t *testing.T) {
	_, err := Open(config.DBConfig{Driver: "mysql", DSN: "x"})
	require.Error(t, err)
}
