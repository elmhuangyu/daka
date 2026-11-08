package api

import (
	"time"

	"github.com/elmhuangyu/daka/backend/lib/db"
	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

const idempotencyKeepFor = 7 * 24 * time.Hour

// PurgeIdempotencyKeys deletes keys older than 7 days and returns the count.
func PurgeIdempotencyKeys(d *db.DB, now time.Time) (int64, error) {
	tr := d.Write().Where("created_at < ?", now.UTC().Add(-idempotencyKeepFor)).Delete(&dbmodels.IdempotencyKey{})
	return tr.RowsAffected, tr.Error
}
