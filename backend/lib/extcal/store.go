package extcal

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
	"github.com/elmhuangyu/daka/backend/lib/secret"
)

// Storage limits.
const (
	MaxSources = 3
	MaxName    = 100
	MaxURLLen  = 2048
)

// InputError is a validation failure safe to show to the client (it never
// echoes submitted values).
type InputError string

func (e InputError) Error() string { return string(e) }

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Input is one submitted calendar source. A nil URL keeps the stored URL
// unchanged (B11): masked hints echoed back by clients therefore never
// overwrite the ciphertext. A non-nil URL replaces it.
type Input struct {
	ID   string
	Name string
	URL  *string
}

// Entry is the stored form of one source. Enc is an AES-GCM envelope; the
// plaintext URL is never persisted.
type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Enc  string `json:"enc"`
}

// View is the client-facing form of a source: no URL, only a hint.
type View struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URLHint string `json:"url_hint"`
}

// Store encrypts, decrypts and validates the external calendar column of
// UserSettings. The zero Keyring (nil) makes every encryption fail with
// secret.ErrNoKey while keeping unchanged sources and listings working.
type Store struct {
	keys      *secret.Keyring
	allowHTTP bool
}

// NewStore builds a Store. keys may be nil.
func NewStore(keys *secret.Keyring, allowHTTP bool) *Store {
	return &Store{keys: keys, allowHTTP: allowHTTP}
}

func aad(userID, id string) string {
	return "user:" + userID + ":external_calendar:" + id
}

// Entries parses the stored JSON column.
func Entries(column string) ([]Entry, error) {
	if strings.TrimSpace(column) == "" {
		return nil, nil
	}
	var es []Entry
	if err := json.Unmarshal([]byte(column), &es); err != nil {
		return nil, errors.New("extcal: corrupt external calendar settings")
	}
	return es, nil
}

// URL decrypts the URL of e.
func (s *Store) URL(userID string, e Entry) (string, error) {
	pt, err := s.keys.Decrypt(e.Enc, aad(userID, e.ID))
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Views renders the stored sources for clients. A source whose URL cannot be
// decrypted (no key, rotated key) is listed with an empty hint.
func (s *Store) Views(userID, column string) ([]View, error) {
	es, err := Entries(column)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(es))
	for _, e := range es {
		v := View{ID: e.ID, Name: e.Name}
		if u, err := s.URL(userID, e); err == nil {
			v.URLHint = Hint(u)
		}
		out = append(out, v)
	}
	return out, nil
}

// Apply merges inputs into the stored column and returns the new column value.
// Sources not mentioned are removed; sources without URL keep their ciphertext.
func (s *Store) Apply(userID, column string, in []Input) (string, error) {
	if len(in) > MaxSources {
		return "", InputError("too many calendar sources")
	}
	existing, err := Entries(column)
	if err != nil {
		return "", err
	}
	byID := map[string]Entry{}
	for _, e := range existing {
		byID[e.ID] = e
	}
	out := make([]Entry, 0, len(in))
	seen := map[string]bool{}
	for _, it := range in {
		name := strings.TrimSpace(it.Name)
		if name == "" || utf8.RuneCountInString(name) > MaxName {
			return "", InputError("invalid calendar name")
		}
		id := it.ID
		switch {
		case id == "":
			var err error
			if id, err = newID(); err != nil {
				return "", err
			}
		case !idPattern.MatchString(id):
			return "", InputError("invalid calendar id")
		}
		if seen[id] {
			return "", InputError("duplicate calendar id")
		}
		seen[id] = true
		cur, known := byID[id]
		if it.URL == nil {
			if !known {
				return "", InputError("url is required for a new calendar")
			}
			out = append(out, Entry{ID: id, Name: name, Enc: cur.Enc})
			continue
		}
		u := strings.TrimSpace(*it.URL)
		if err := s.validateURL(u); err != nil {
			return "", err
		}
		if known {
			// A client echoing the masked hint must never replace the secret.
			if old, err := s.URL(userID, cur); err == nil && u == Hint(old) {
				return "", InputError("invalid calendar url")
			}
		}
		enc, err := s.keys.Encrypt([]byte(u), aad(userID, id))
		if err != nil {
			return "", err
		}
		out = append(out, Entry{ID: id, Name: name, Enc: enc})
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func newID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("extcal: random source failure")
	}
	return hex.EncodeToString(b), nil
}

// validateURL performs the cheap, syntactic checks done at save time; the
// authoritative address checks happen at connect time in the Fetcher.
func (s *Store) validateURL(raw string) error {
	bad := InputError("invalid calendar url")
	if raw == "" || len(raw) > MaxURLLen ||
		strings.ContainsAny(raw, "\r\n\t ") ||
		strings.Contains(raw, "…") || strings.Contains(raw, "***") || strings.Contains(raw, "...") {
		return bad
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return bad
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !s.allowHTTP {
			return InputError("calendar url must use https")
		}
	default:
		return InputError("calendar url must use https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		return InputError("calendar url host is not allowed")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !IPAllowed(ip) {
		return InputError("calendar url host is not allowed")
	}
	return nil
}

// Hint is the masked form returned to clients: scheme://host/… only. Neither
// credentials, path nor query (which usually hold the secret key) appear.
func Hint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// ReencryptAll re-seals every stored URL that was written with a retired key
// under the current key (key rotation). It returns the number of updated users.
func (s *Store) ReencryptAll(db *gorm.DB) (int, error) {
	var rows []dbmodels.UserSettings
	if err := db.Where("external_calendar_urls <> ''").Find(&rows).Error; err != nil {
		return 0, err
	}
	updated := 0
	for _, row := range rows {
		es, err := Entries(row.ExternalCalendarURLs)
		if err != nil {
			return updated, err
		}
		changed := false
		for i, e := range es {
			enc, ch, err := s.keys.Reencrypt(e.Enc, aad(row.UserID, e.ID))
			if err != nil {
				return updated, err
			}
			if ch {
				es[i].Enc, changed = enc, true
			}
		}
		if !changed {
			continue
		}
		b, err := json.Marshal(es)
		if err != nil {
			return updated, err
		}
		if err := db.Model(&dbmodels.UserSettings{}).Where("user_id = ?", row.UserID).
			Update("external_calendar_urls", string(b)).Error; err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
