package storage

import (
	"context"
	"time"
)

// Service defines all database operations the application needs.
// Other packages depend on this interface, not on GORM directly.
// Any struct whose methods match this list satisfies the interface automatically -
// no "implements" keyword needed (this is called "structural typing").
type Service interface {
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetNotificationByID(ctx context.Context, id uint) (*Notification, error)
	GetNotificationByMessageID(ctx context.Context, messageID int) (*Notification, error)
	CreateUser(ctx context.Context, user *User) error
	CreateOrUpdateUser(ctx context.Context, user *User) error
	UpdateUserChatID(ctx context.Context, telegramID, chatID int64) error
	UpdateUserNames(ctx context.Context, telegramID int64, firstName, username string) error
	UpdateNotificationMessageID(ctx context.Context, notificationID uint, messageID int) error
	HasNotification(ctx context.Context, chatID int64, showTitle string, season, episodeNumber int) (bool, error)
	CreateNotification(ctx context.Context, notification *Notification) error
	HasUserInChat(ctx context.Context, chatID int64) (bool, error)
	GetTopics(ctx context.Context, chatID int64) ([]Topic, error)
	CreateOrUpdateTopic(ctx context.Context, topic *Topic) error
	UpdateUserTokens(ctx context.Context, telegramID int64, accessToken, refreshToken string, expiresAt time.Time) error
	UpdateUserMuted(ctx context.Context, telegramID int64, muted bool) error
	GetDistinctChatIDs(ctx context.Context) ([]int64, error)
	GetUsersByChatID(ctx context.Context, chatID int64) ([]User, error)

	// WatchStatus methods - track per-user watched state on episode notifications
	CreateWatchStatuses(ctx context.Context, notificationID uint, userIDs []uint) error
	GetWatchStatuses(ctx context.Context, notificationID uint) ([]WatchStatus, error)
	GetUserWatchStatus(ctx context.Context, notificationID uint, userID uint) (WatchStatus, error)
	GetUnwatchedStatusesByUser(ctx context.Context, userID uint) ([]WatchStatus, error)
	MarkWatchStatus(ctx context.Context, notificationID uint, userID uint) error
	UnmarkWatchStatus(ctx context.Context, notificationID uint, userID uint) error

	// ChatConfig methods - per-chat settings (country, timezone, deletion toggle)
	GetChatConfig(ctx context.Context, chatID int64) (*ChatConfig, error)
	CreateOrUpdateChatConfig(ctx context.Context, config *ChatConfig) error

	// ScheduledDeletion methods - deferred message cleanup
	CreateScheduledDeletion(ctx context.Context, deletion *ScheduledDeletion) error
	GetPendingDeletions(ctx context.Context) ([]ScheduledDeletion, error)
	RemoveScheduledDeletion(ctx context.Context, id uint) error
}
