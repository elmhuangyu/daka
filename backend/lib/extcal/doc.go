// Package extcal implements the external calendar overlay: an SSRF-safe
// iCalendar fetcher, a minimal iCalendar parser, encrypted per-user feed URL
// storage and the cached multi-source service behind /api/calendar/external.
//
// Feed URLs are secrets (they often embed an access key): they are stored only
// as AES-GCM ciphertext, never logged, and never part of an error message.
package extcal
