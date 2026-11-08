package dbmodels

import "time"

// IdempotencyKey stores the first response of a write request so that replays
// with the same Idempotency-Key header return it verbatim. Rows are kept for 7
// days (see api.PurgeIdempotencyKeys).
type IdempotencyKey struct {
	UserID       string `gorm:"primaryKey;size:64"`
	Key          string `gorm:"primaryKey;size:255"`
	StatusCode   int    `gorm:"not null"`
	ResponseBody string `gorm:"type:text"`
	CreatedAt    time.Time
}
