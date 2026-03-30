package storage

import (
	"fmt"
	"time"

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

// ProviderInfo holds a streaming service name and URL for JSON storage.
type ProviderInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Notification tracks episodes we've already notified about,
// so we don't send duplicates to the group chat.
// One notification per show+episode combo per chat - shared across all users in that chat.
// Stores all episode data needed to reconstruct the notification message for editing.
type Notification struct {
	gorm.Model
	// Composite unique index: one notification per show+episode combo per chat.
	ChatID            int64  `gorm:"uniqueIndex:idx_chat_episode"`
	ShowTitle         string `gorm:"uniqueIndex:idx_chat_episode"`
	Season            int    `gorm:"uniqueIndex:idx_chat_episode"`
	EpisodeNumber     int    `gorm:"uniqueIndex:idx_chat_episode"`
	TraktShowID       int
	TelegramMessageID int

	// Episode details from Trakt - stored so we can rebuild the message when editing
	EpisodeTitle string
	FirstAired   string
	Runtime      int
	Rating       float64
	ShowSlug     string
	IMDBID       string
	PhotoURL     string

	// Watch providers from TMDB - serializer:"json" stores the Go slice as a JSON
	// string in a single text column. GORM handles marshal/unmarshal automatically.
	Providers []ProviderInfo `gorm:"serializer:json"`
	WatchLink string         // JustWatch URL for the show
}

// ScheduledDeletion records a Telegram message that should be deleted after a delay.
// The deletion checker periodically queries for rows where DeleteAt has passed.
type ScheduledDeletion struct {
	gorm.Model
	NotificationID uint  `gorm:"uniqueIndex"` // one pending deletion per notification
	ChatID         int64 // Telegram chat containing the message
	MessageID      int   // Telegram message ID to delete
	DeleteAt       time.Time
}

// ChatConfig stores per-chat settings like country, timezone, and deletion preferences.
// One row per chat - uniqueIndex on ChatID enforces this at the database level.
type ChatConfig struct {
	gorm.Model
	ChatID        int64  `gorm:"uniqueIndex"` // one config per chat
	Country       string // ISO 3166-1 alpha-2 country code, e.g. "US", "GB"
	Timezone      string // IANA timezone, e.g. "America/New_York"
	DeleteWatched bool   // when true, delete episode messages after all users have watched
}

// WatchStatus tracks whether a specific user has watched a notified episode.
// This is a "join table" - it links a Notification to a User, with an extra
// Watched flag. Used to render the "Watched by: @user ✅  @other ⏳" line
// at the bottom of episode notification messages.
type WatchStatus struct {
	gorm.Model
	// Composite unique index: one row per user per notification.
	NotificationID uint `gorm:"uniqueIndex:idx_notification_user"`
	UserID         uint `gorm:"uniqueIndex:idx_notification_user"`
	Watched        bool // false = ⏳ (pending), true = ✅ (watched)

	// GORM associations - lets us eager-load related records with Preload().
	// "foreignKey:..." tells GORM which field in this struct points to the related table's primary key.
	User         User         `gorm:"foreignKey:UserID"`
	Notification Notification `gorm:"foreignKey:NotificationID"`
}
