// Package models contains domain structures shared across packages.
package models

// User represents a registered bot user.
type User struct {
	UID        string // UUIDv4, generated at registration
	TelegramID int64  // Telegram user ID
	Username   string // Telegram login (@username, without @)
	FirstName  string
	LastName   string
	Phone      string // filled after the user shares it via the contact button
	CreatedAt  string
}
