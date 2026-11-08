package stats

import (
	"container/list"
	"fmt"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// DefaultCacheSize is the default LRU capacity.
const DefaultCacheSize = 2000

// Key identifies a cached Stats value. Every field that can change the result
// is part of the key, so a stale value can never be served (DESIGN 8):
//   - LogSig: in-process per-item version counter (Versions) plus
//     COUNT/MAX(id)/SUM(value) of the item's logs read from the database, so
//     writes from other processes or code paths that forgot to Bump still miss;
//   - RuleSig: max rule version id (+ option counts);
//   - Today: the user-local date, so a day passing breaks streaks;
//   - StateVersion: UserSettings.state_version, bumped on freeze changes;
//   - SettingsSig: timezone / week start / holiday region / holiday freeze read
//     from the settings row, so a settings change misses even if it did not bump.
type Key struct {
	UserID       string
	ItemID       uint
	LogSig       string
	RuleSig      string
	Today        string
	StateVersion int64
	SettingsSig  string
}

type entry struct {
	key Key
	val Stats
}

// Cache is a bounded, concurrency-safe LRU of Stats.
type Cache struct {
	mu     sync.Mutex
	max    int
	ll     *list.List
	items  map[Key]*list.Element
	hits   atomic.Int64
	misses atomic.Int64
}

// NewCache returns a cache holding at most max entries (<= 0: DefaultCacheSize).
func NewCache(max int) *Cache {
	if max <= 0 {
		max = DefaultCacheSize
	}
	return &Cache{max: max, ll: list.New(), items: map[Key]*list.Element{}}
}

// Get returns the cached value and counts a hit or miss.
func (c *Cache) Get(k Key) (Stats, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.ll.MoveToFront(el)
		c.hits.Add(1)
		return el.Value.(*entry).val, true
	}
	c.misses.Add(1)
	return Stats{}, false
}

// Put stores v, evicting the least recently used entry beyond capacity.
func (c *Cache) Put(k Key, v Stats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		el.Value.(*entry).val = v
		c.ll.MoveToFront(el)
		return
	}
	c.items[k] = c.ll.PushFront(&entry{key: k, val: v})
	for c.ll.Len() > c.max {
		old := c.ll.Back()
		c.ll.Remove(old)
		delete(c.items, old.Value.(*entry).key)
	}
}

// Len is the number of cached entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Hits is the number of cache hits so far (for tests and metrics).
func (c *Cache) Hits() int64 { return c.hits.Load() }

// Misses is the number of cache misses so far.
func (c *Cache) Misses() int64 { return c.misses.Load() }

// Versions is a per-process, per-item write counter. Check-in write and
// delete paths call Bump; it is only one component of the log signature.
type Versions struct {
	mu sync.Mutex
	m  map[uint]uint64
}

// NewVersions returns an empty counter set.
func NewVersions() *Versions { return &Versions{m: map[uint]uint64{}} }

// Bump increments the item's counter. Safe on a nil receiver.
func (v *Versions) Bump(itemID uint) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.m[itemID]++
	v.mu.Unlock()
}

// Get returns the item's counter. Safe on a nil receiver.
func (v *Versions) Get(itemID uint) uint64 {
	if v == nil {
		return 0
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.m[itemID]
}

// LogSignature combines the in-process counter with the database-derived
// COUNT(*), MAX(id) and SUM(value) of the item's logs.
func LogSignature(tx *gorm.DB, v *Versions, itemID uint) (string, error) {
	var r struct {
		N   int64
		Max int64
		Sum float64
	}
	err := tx.Model(&dbmodels.CheckinLog{}).Where("item_id = ?", itemID).
		Select("COUNT(*) AS n, COALESCE(MAX(id), 0) AS max, COALESCE(SUM(value), 0) AS sum").Scan(&r).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v%d/n%d/m%d/s%g", v.Get(itemID), r.N, r.Max, r.Sum), nil
}

// RuleSignature is the max rule version id of the item plus option counts
// (archiving an option changes completion without a new rule version).
func RuleSignature(tx *gorm.DB, itemID uint) (string, error) {
	var maxVer int64
	if err := tx.Model(&dbmodels.ItemRuleVersion{}).Where("item_id = ?", itemID).
		Select("COALESCE(MAX(id), 0)").Scan(&maxVer).Error; err != nil {
		return "", err
	}
	var o struct {
		N        int64
		Archived int64
		Max      int64
	}
	if err := tx.Model(&dbmodels.ItemOption{}).Where("item_id = ?", itemID).
		Select("COUNT(*) AS n, COALESCE(SUM(CASE WHEN archived THEN 1 ELSE 0 END), 0) AS archived, COALESCE(MAX(id), 0) AS max").
		Scan(&o).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("r%d/o%d/a%d/m%d", maxVer, o.N, o.Archived, o.Max), nil
}
