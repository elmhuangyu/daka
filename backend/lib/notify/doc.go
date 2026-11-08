// Package notify delivers Daka reminders through a Telegram bot (DESIGN 7.5).
//
// Telegram is notify-only: the bot sends a per-user daily digest and optional
// per-item reminders, and the only thing it reads is the one-time link code a
// user sends to bind their chat. There are no inline buttons and no write path
// into check-ins. The bot token comes from the environment only, updates are
// fetched by long polling (no public webhook route exists) and message bodies
// never contain the names of sensitive items.
package notify
