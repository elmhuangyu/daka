package notify

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/elmhuangyu/daka/backend/lib/dbmodels"
)

// LinkCodeTTL is how long a link code stays valid (DESIGN 7.5).
const LinkCodeTTL = 10 * time.Minute

// Binding errors.
var (
	ErrInvalidCode = errors.New("notify: link code invalid or expired")
	ErrChatTaken   = errors.New("notify: chat already linked to another user")
	ErrInvalidTime = errors.New("notify: digest time must be HH:MM or empty")
)

func hashCode(code string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(h[:])
}

// CreateLinkCode issues a one-time code (128 random bits; only the hash is
// stored) and invalidates the user's earlier codes. Expired codes of all users
// are swept on the way.
func CreateLinkCode(g *gorm.DB, userID string, now time.Time) (code string, expires time.Time, err error) {
	var b [16]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	code = base64.RawURLEncoding.EncodeToString(b[:])
	expires = now.Add(LinkCodeTTL).UTC()
	err = g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? OR expires_at <= ?", userID, now.UTC()).Delete(&dbmodels.TelegramLinkCode{}).Error; err != nil {
			return err
		}
		return tx.Create(&dbmodels.TelegramLinkCode{CodeHash: hashCode(code), UserID: userID, ExpiresAt: expires}).Error
	})
	return code, expires, err
}

// Bind consumes a link code and binds chatID to the code's user. A chat that
// is already linked to a different user is refused (the code is kept so the
// user can retry from another chat). Linking again replaces the user's chat
// and keeps their settings.
func Bind(g *gorm.DB, code string, chatID int64, now time.Time) (*dbmodels.TelegramLink, error) {
	var out dbmodels.TelegramLink
	err := g.Transaction(func(tx *gorm.DB) error {
		var c dbmodels.TelegramLinkCode
		err := tx.Where("code_hash = ? AND expires_at > ?", hashCode(code), now.UTC()).Take(&c).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidCode
		}
		if err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&dbmodels.TelegramLink{}).Where("chat_id = ? AND user_id <> ?", chatID, c.UserID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrChatTaken
		}
		err = tx.Where("user_id = ?", c.UserID).Take(&out).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			out = dbmodels.TelegramLink{UserID: c.UserID, ChatID: chatID, Enabled: true, RemindSensitive: true,
				DigestTime: dbmodels.DefaultDigestTime, LinkedAt: now.UTC()}
			if err := tx.Create(&out).Error; err != nil {
				return mapUnique(err)
			}
		case err != nil:
			return err
		default:
			out.ChatID, out.Enabled, out.LinkedAt = chatID, true, now.UTC()
			if err := tx.Save(&out).Error; err != nil {
				return mapUnique(err)
			}
		}
		return tx.Delete(&dbmodels.TelegramLinkCode{}, "code_hash = ?", c.CodeHash).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// mapUnique turns a chat_id unique violation (a concurrent bind) into ErrChatTaken.
func mapUnique(err error) error {
	if s := strings.ToLower(err.Error()); strings.Contains(s, "unique") || strings.Contains(s, "duplicate") {
		return ErrChatTaken
	}
	return err
}

// Unlink removes the user's chat binding and any pending codes.
func Unlink(g *gorm.DB, userID string) error {
	return g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&dbmodels.TelegramLinkCode{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&dbmodels.TelegramLink{}).Error
	})
}

// GetLink returns the user's link, or nil when unlinked.
func GetLink(g *gorm.DB, userID string) (*dbmodels.TelegramLink, error) {
	var l dbmodels.TelegramLink
	err := g.Where("user_id = ?", userID).Take(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// SettingsPatch is a partial update of the notification preferences.
type SettingsPatch struct {
	Enabled         *bool
	RemindSensitive *bool
	// DigestTime is "HH:MM", or "" to disable the digest.
	DigestTime *string
}

// ValidClock reports whether s is a 24-hour "HH:MM" time.
func ValidClock(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	t, err := time.Parse("15:04", s)
	return err == nil && t.Format("15:04") == s
}

// UpdateSettings applies the patch; it returns gorm.ErrRecordNotFound when the
// user has no link.
func UpdateSettings(g *gorm.DB, userID string, p SettingsPatch) (*dbmodels.TelegramLink, error) {
	if p.DigestTime != nil && *p.DigestTime != "" && !ValidClock(*p.DigestTime) {
		return nil, ErrInvalidTime
	}
	var l dbmodels.TelegramLink
	if err := g.Where("user_id = ?", userID).Take(&l).Error; err != nil {
		return nil, err
	}
	if p.Enabled != nil {
		l.Enabled = *p.Enabled
	}
	if p.RemindSensitive != nil {
		l.RemindSensitive = *p.RemindSensitive
	}
	if p.DigestTime != nil {
		l.DigestTime = *p.DigestTime
	}
	if err := g.Save(&l).Error; err != nil {
		return nil, fmt.Errorf("save telegram settings: %w", err)
	}
	return &l, nil
}
