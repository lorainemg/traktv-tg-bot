package storage

// Service defines all database operations the application needs.
// Other packages depend on this interface, not on GORM directly.
// Any struct whose methods match this list satisfies the interface automatically —
// no "implements" keyword needed (this is called "structural typing").
type Service interface {
	GetAllUsers() ([]User, error)
	GetUserByTelegramID(telegramID int64) (*User, error)
	CreateUser(user *User) error
	CreateOrUpdateUser(user *User) error
	UpdateUserChatID(telegramID, chatID int64) error
	UpdateUserNames(telegramID int64, firstName, username string) error
	HasNotification(userID uint, showTitle, episodeKey string) (bool, error)
	CreateNotification(notification *Notification) error
	HasUserInChat(chatID int64) (bool, error)
	GetTopics(chatID int64) ([]Topic, error)
	CreateOrUpdateTopic(topic *Topic) error
	UpdateUserMuted(telegramID int64, muted bool) error
}