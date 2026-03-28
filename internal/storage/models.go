package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// User represents a Telegram user who has linked their Trakt.tv account.
// GORM maps this struct to a "users" table automatically.
type User struct {
	gorm.Model              // embeds ID, CreatedAt, UpdatedAt, DeletedAt
	TelegramID        int64 `gorm:"uniqueIndex"`
	FirstName         string
	Username          string
	ChatID            int64 // Telegram chat where this user authenticated
	TraktAccessToken  string
	TraktRefreshToken string
	Muted             bool // when true, the user won't receive episode notifications
}

// MentionLink returns a clickable Markdown mention for this user.
// Prefers @username with an https://t.me link; falls back to FirstName
// or "User" with tg://user deep link when no username is set.
func (u *User) MentionLink() string {
	if u.Username != "" {
		return fmt.Sprintf("[@%s](https://t.me/%s)", u.Username, u.Username)
	}
	name := u.FirstName
	if name == "" {
		name = "User"
	}
	return fmt.Sprintf("[%s](tg://user?id=%d)", name, u.TelegramID)
}

// Topic maps a Telegram forum topic (thread) to a name for routing notifications.
// For example, a topic named "anime" in a group chat receives anime episode updates.
type Topic struct {
	gorm.Model
	ChatID   int64  `gorm:"uniqueIndex:idx_chat_topic"` // Telegram chat ID
	ThreadID int    `gorm:"uniqueIndex:idx_chat_topic"` // forum topic's message_thread_id
	Name     string // lowercase topic name, e.g. "anime", "tv shows"
}

// Notification tracks episodes we've already notified about,
// so we don't send duplicates to the group chat.
// One notification per show+episode combo per chat — shared across all users in that chat.
type Notification struct {
	gorm.Model
	// Composite unique index: one notification per show+episode combo per chat.
	ChatID            int64  `gorm:"uniqueIndex:idx_chat_episode"`
	ShowTitle         string `gorm:"uniqueIndex:idx_chat_episode"`
	Season            int    `gorm:"uniqueIndex:idx_chat_episode"`
	EpisodeNumber     int    `gorm:"uniqueIndex:idx_chat_episode"`
	TraktShowID       int
	TelegramMessageID int
}

func (n *Notification) EpisodeKey() string {
	return fmt.Sprintf("S%02dE%02d", n.Season, n.EpisodeNumber)
}
