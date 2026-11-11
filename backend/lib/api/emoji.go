package api

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxEmojiBytes = 32

// validEmoji accepts "" (no emoji) or a short symbol string such as "🥤" or a ZWJ sequence.
// It rejects plain ASCII, whitespace, control characters and anything too long; it does not enumerate emoji.
func validEmoji(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > maxEmojiBytes || utf8.RuneCountInString(s) > 12 || !utf8.ValidString(s) {
		return false
	}
	if strings.TrimSpace(s) != s {
		return false
	}
	ascii := true
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
		if r >= 0x80 {
			ascii = false
		}
	}
	// Plain ASCII text is not an emoji (keycaps such as "1️⃣" carry non-ASCII selectors).
	return !ascii
}
