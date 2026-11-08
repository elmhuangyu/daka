package db

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/config"
)

type role int

const (
	roleWrite role = iota
	roleRead
)

// poolConfig is the database/sql pool configuration for one pool.
type poolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

const (
	sqliteBusyTimeoutMs = 10000
	sqliteReadConns     = 4
)

// poolConfigFor returns pool settings per driver and role. The single-writer
// strategy (MaxOpenConns=1) applies to SQLite only; PostgreSQL uses a regular
// pool for both roles (DESIGN 8, C5).
func poolConfigFor(driver string, r role) poolConfig {
	if driver == config.DriverSQLite {
		if r == roleWrite {
			return poolConfig{MaxOpenConns: 1, MaxIdleConns: 1}
		}
		return poolConfig{MaxOpenConns: sqliteReadConns, MaxIdleConns: sqliteReadConns}
	}
	return poolConfig{MaxOpenConns: 25, MaxIdleConns: 5, ConnMaxLifetime: 30 * time.Minute}
}

// dialectDSN derives the driver-level DSN for a role. PostgreSQL DSNs are
// passed through untouched: no SQLite pragmas or options are ever applied.
func dialectDSN(driver, dsn string, r role) string {
	if driver != config.DriverSQLite {
		return dsn
	}
	q := url.Values{}
	if r == roleRead {
		q.Add("mode", "ro")
	}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeoutMs))
	if r == roleWrite {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
		q.Add("_pragma", "foreign_keys(1)")
	} else {
		q.Add("_pragma", "query_only(1)")
		q.Add("_pragma", "foreign_keys(1)")
	}
	u := url.URL{Scheme: "file", Opaque: (&url.URL{Path: dsn}).EscapedPath(), RawQuery: q.Encode()}
	return u.String()
}

// DB holds the write pool and the read pool. With PostgreSQL both point at
// the same pool.
type DB struct {
	write, read *gorm.DB
	closers     []*sql.DB
}

// Write returns the handle for writes (and read-your-writes reads).
func (d *DB) Write() *gorm.DB { return d.write }

// Read returns the handle for read-only queries (aggregation, ICS, ...).
func (d *DB) Read() *gorm.DB { return d.read }

// Close closes all underlying pools.
func (d *DB) Close() error {
	var errs []error
	for _, s := range d.closers {
		errs = append(errs, s.Close())
	}
	return errors.Join(errs...)
}

func gormConfig() *gorm.Config {
	return &gorm.Config{
		Logger: NewLogger(
			WithDefaultLogLevel(zerolog.InfoLevel),
			WithSlowThreshold(200*time.Millisecond),
			WithIgnoreNotFoundError(),
		),
	}
}

func openPool(driver, dsn string, r role) (*gorm.DB, *sql.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case config.DriverSQLite:
		dialector = sqlite.Open(dialectDSN(driver, dsn, r))
	case config.DriverPostgres:
		dialector = postgres.Open(dialectDSN(driver, dsn, r))
	default:
		return nil, nil, fmt.Errorf("unsupported db driver %q", driver)
	}
	g, err := gorm.Open(dialector, gormConfig())
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, nil, err
	}
	pc := poolConfigFor(driver, r)
	sqlDB.SetMaxOpenConns(pc.MaxOpenConns)
	sqlDB.SetMaxIdleConns(pc.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(pc.ConnMaxLifetime)
	return g, sqlDB, nil
}

// Open opens the configured database.
func Open(c config.DBConfig) (*DB, error) {
	dsn := c.DSN.Reveal()
	w, wsql, err := openPool(c.Driver, dsn, roleWrite)
	if err != nil {
		return nil, fmt.Errorf("open write pool: %w", err)
	}
	if err := wsql.Ping(); err != nil { // creates the file and enables WAL
		_ = wsql.Close()
		return nil, fmt.Errorf("ping write pool: %w", err)
	}
	if c.Driver != config.DriverSQLite {
		return &DB{write: w, read: w, closers: []*sql.DB{wsql}}, nil
	}
	r, rsql, err := openPool(c.Driver, dsn, roleRead)
	if err != nil {
		_ = wsql.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	return &DB{write: w, read: r, closers: []*sql.DB{wsql, rsql}}, nil
}
