package dbmodels

import "time"

// DefaultDigestTime is the local time of the daily Telegram digest.
const DefaultDigestTime = "08:00"

// TelegramLink binds a user to their Telegram bot and chat (notify only: nothing is ever
// written to Daka from Telegram except the one-time link code).
type TelegramLink struct {
	UserID string `gorm:"primaryKey;size:64"`
	// TokenEnc is the user's encrypted Telegram bot token (AES-GCM envelope).
	TokenEnc string
	// BotUsername is the username of the bot (without @).
	BotUsername string `gorm:"size:64"`
	ChatID      int64  `gorm:"not null;uniqueIndex"`
	Enabled     bool   `gorm:"not null"`
	// RemindSensitive true sends sensitive items with a generic redacted text;
	// false leaves them out of messages entirely.
	RemindSensitive bool `gorm:"not null"`
	// DigestTime is the local "HH:MM" of the daily digest in
	// UserSettings.timezone; empty disables the digest.
	DigestTime string `gorm:"size:5"`
	// Lang is the language of bot messages ("en", "zh-CN"); empty means English.
	Lang     string `gorm:"size:8"`
	LinkedAt time.Time
}

// TelegramLinkCode is a one-time link code; only its hash is stored.
type TelegramLinkCode struct {
	CodeHash    string `gorm:"primaryKey;size:128"`
	UserID      string `gorm:"size:64;not null;index"`
	TokenEnc    string
	BotUsername string    `gorm:"size:64"`
	ExpiresAt   time.Time `gorm:"not null"`
}

// ReminderLog deduplicates reminders. ItemID 0 is the user's daily digest.
// The row is the claim made before sending, so restarts never re-send.
type ReminderLog struct {
	UserID     string `gorm:"primaryKey;size:64"`
	ItemID     uint   `gorm:"primaryKey;autoIncrement:false"`
	RemindDate string `gorm:"primaryKey;size:10"`
	SentAt     time.Time
}
