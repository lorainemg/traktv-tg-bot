package storage

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// NotificationType distinguishes between episode and movie notifications.
// Used as a discriminator in WatchStatus so both content types share
// one watch-tracking table. Stored as a string in the DB for readability.
type NotificationType string

const (
	NotificationEpisode NotificationType = "episode"
	NotificationMovie   NotificationType = "movie"
)

// User represents a Telegram user who has linked their Trakt.tv account.
// GORM maps this struct to a "users" table automatically.
type User struct {
	gorm.Model                // embeds ID, CreatedAt, UpdatedAt, DeletedAt
	TelegramID          int64 `gorm:"uniqueIndex"`
	FirstName           string
	Username            string
	ChatID              int64 // Telegram chat where this user authenticated
	TraktAccessToken    string
	TraktRefreshToken   string
	TraktTokenExpiresAt time.Time // when the access token expires - used to trigger refresh
	Muted               bool      // when true, the user won't receive episode notifications
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

// Mention returns a plain @username (with underscores escaped for MarkdownV1)
// or falls back to a clickable link for users without a username.
func (u *User) Mention() string {
	if u.Username != "" {
		// Telegram usernames can contain underscores, which MarkdownV1 parses
		// as italic markers. Escaping with backslash prevents that.
		escaped := strings.ReplaceAll(u.Username, "_", "\\_")
		return fmt.Sprintf("@%s", escaped)
	}
	return u.MentionLink()
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
	NotifyHours   int    // how many hours before air time to include in notifications (0 = use default)
}

// WatchStatus tracks whether a specific user has watched a notified episode or movie.
// This is a "join table" - it links a notification (episode or movie) to a User,
// with an extra Watched flag. NotificationType + NotificationID together identify
// which notification this refers to.
type WatchStatus struct {
	gorm.Model
	// Composite unique index: one row per user per notification per type.
	NotificationType NotificationType `gorm:"uniqueIndex:idx_type_notification_user"`
	NotificationID   uint             `gorm:"uniqueIndex:idx_type_notification_user"`
	UserID           uint             `gorm:"uniqueIndex:idx_type_notification_user"`
	Watched          bool             // false = ⏳ (pending), true = ✅ (watched)

	// GORM associations - lets us eager-load related records with Preload().
	// "foreignKey:..." tells GORM which field in this struct points to the related table's primary key.
	User         User         `gorm:"foreignKey:UserID"`
	Notification Notification `gorm:"foreignKey:NotificationID"`
}

// MovieSubscription tracks which users subscribe to trending movie notifications.
// Type distinguishes between "all" (all trending) and "available" (digital/physical released).
type MovieSubscription struct {
	gorm.Model
	UserID uint   `gorm:"uniqueIndex:idx_user_sub_type"` // one subscription per type per user
	ChatID int64  // group chat where followed movies get posted
	Type   string `gorm:"uniqueIndex:idx_user_sub_type"` // "all" or "available"
	User   User   `gorm:"foreignKey:UserID"`
}

// FollowedMovie tracks movies a user has already seen in trending lists
// (followed or skipped). Used for deduplication so the same movie
// doesn't appear in future weekly trending lists.
type FollowedMovie struct {
	gorm.Model
	UserID       uint `gorm:"uniqueIndex:idx_user_movie"` // one entry per movie per user
	TraktMovieID int  `gorm:"uniqueIndex:idx_user_movie"`
	User         User `gorm:"foreignKey:UserID"`
}

// MovieNotification tracks movies posted to the group chat after a user
// clicks Follow. One notification per movie per chat — shared across users.
// Stores all movie data needed to reconstruct the message for editing.
type MovieNotification struct {
	gorm.Model
	ChatID            int64 `gorm:"uniqueIndex:idx_chat_movie"` // one notification per movie per chat
	TraktMovieID      int   `gorm:"uniqueIndex:idx_chat_movie"`
	MovieTitle        string
	Year              int
	Genre             string
	Runtime           int
	Rating            float64
	MovieSlug         string
	IMDBID            string
	TelegramMessageID int
}
