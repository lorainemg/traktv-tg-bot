package storage

// Service defines all database operations the application needs.
// Other packages depend on this interface, not on GORM directly.
// Any struct whose methods match this list satisfies the interface automatically —
// no "implements" keyword needed (this is called "structural typing").
type Service interface {
GetUserByTelegramID(telegramID int64) (*User, error)
	GetNotificationByMessageID(messageID int) (*Notification, error)
	CreateUser(user *User) error
	CreateOrUpdateUser(user *User) error
	UpdateUserChatID(telegramID, chatID int64) error
	UpdateUserNames(telegramID int64, firstName, username string) error
	UpdateNotificationMessageID(notificationID uint, messageID int) error
	HasNotification(chatID int64, showTitle string, season, episodeNumber int) (bool, error)
	CreateNotification(notification *Notification) error
	HasUserInChat(chatID int64) (bool, error)
	GetTopics(chatID int64) ([]Topic, error)
	CreateOrUpdateTopic(topic *Topic) error
	UpdateUserMuted(telegramID int64, muted bool) error
	GetDistinctChatIDs() ([]int64, error)
	GetUsersByChatID(chatID int64) ([]User, error)

	// WatchStatus methods — track per-user watched state on episode notifications
	CreateWatchStatuses(notificationID uint, userIDs []uint) error
	GetWatchStatuses(notificationID uint) ([]WatchStatus, error)
	GetUnwatchedStatusesByUser(userID uint) ([]WatchStatus, error)
	MarkWatchStatus(notificationID uint, userID uint) error
}
