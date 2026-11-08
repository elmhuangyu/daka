package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	_ logger.Interface  = Logger{}
	_ gorm.ParamsFilter = Logger{}
)

// Logger bridges GORM logging to zerolog (taken from the context logger).
// Bound parameters are never logged: ParamsFilter always strips them, so
// user data such as item names cannot leak into logs.
type Logger struct {
	opt *options
}

type options struct {
	defaultLogLevel   zerolog.Level
	fieldName         string
	ignoreNotFoundErr bool
	slowThreshold     time.Duration
	logAll            bool
}

// OptionFn configures a Logger.
type OptionFn func(*options)

// WithLogAll logs every query, not only slow or failed ones.
func WithLogAll() OptionFn { return func(o *options) { o.logAll = true } }

// WithIgnoreNotFoundError skips gorm.ErrRecordNotFound.
func WithIgnoreNotFoundError() OptionFn { return func(o *options) { o.ignoreNotFoundErr = true } }

// WithDefaultLogLevel sets the level used for non-slow, non-error queries.
func WithDefaultLogLevel(l zerolog.Level) OptionFn { return func(o *options) { o.defaultLogLevel = l } }

// WithSlowThreshold sets the duration above which a query is logged as a warning.
func WithSlowThreshold(d time.Duration) OptionFn { return func(o *options) { o.slowThreshold = d } }

// NewLogger creates a Logger. Defaults: debug level, 500ms slow threshold.
func NewLogger(opts ...OptionFn) Logger {
	o := &options{defaultLogLevel: zerolog.DebugLevel, fieldName: "sql", slowThreshold: 500 * time.Millisecond}
	for _, fn := range opts {
		fn(o)
	}
	return Logger{opt: o}
}

// LogMode implements logger.Interface; levels are controlled by zerolog.
func (l Logger) LogMode(logger.LogLevel) logger.Interface { return l }

// Error implements logger.Interface.
func (l Logger) Error(ctx context.Context, msg string, args ...any) {
	zerolog.Ctx(ctx).Error().Msg(fmt.Sprintf(msg, args...))
}

// Warn implements logger.Interface.
func (l Logger) Warn(ctx context.Context, msg string, args ...any) {
	zerolog.Ctx(ctx).Warn().Msg(fmt.Sprintf(msg, args...))
}

// Info implements logger.Interface.
func (l Logger) Info(ctx context.Context, msg string, args ...any) {
	zerolog.Ctx(ctx).Info().Msg(fmt.Sprintf(msg, args...))
}

// Trace implements logger.Interface: it logs failed queries as errors, slow
// queries as warnings, and everything else only when logAll is set.
func (l Logger) Trace(ctx context.Context, begin time.Time, f func() (string, int64), err error) {
	elapsed := time.Since(begin)
	zl := zerolog.Ctx(ctx)
	var ev *zerolog.Event
	switch {
	case err != nil:
		if l.opt.ignoreNotFoundErr && errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		ev = zl.Error().Err(err)
	case l.opt.slowThreshold > 0 && elapsed > l.opt.slowThreshold:
		ev = zl.Warn()
	case l.opt.logAll:
		ev = zl.WithLevel(l.opt.defaultLogLevel)
	default:
		return
	}
	sql, rows := f()
	ev = ev.Int64("elapsed_ms", elapsed.Milliseconds())
	if sql != "" {
		ev = ev.Str(l.opt.fieldName, sql)
	}
	if rows > -1 {
		ev = ev.Int64("rows_affected", rows)
	}
	ev.Send()
}

// ParamsFilter strips all bound parameters so the SQL passed to Trace
// contains placeholders only.
func (l Logger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}
