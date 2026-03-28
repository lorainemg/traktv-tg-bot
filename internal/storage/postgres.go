package storage

import (
	"errors"
	"fmt"

	"gorm.io/driver/postgres"
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

// Topic maps a Telegram forum topic (thread) to a name for routing notifications.
// For example, a topic named "anime" in a group chat receives anime episode updates.
type Topic struct {
	gorm.Model
	ChatID   int64  `gorm:"uniqueIndex:idx_chat_topic"` // Telegram chat ID
	ThreadID int    `gorm:"uniqueIndex:idx_chat_topic"` // forum topic's message_thread_id
	Name     string // lowercase topic name, e.g. "anime", "tv shows"
}

// Notification tracks episodes we've already notified about,
// so we don't send duplicates to the group.
type Notification struct {
	gorm.Model
	// Composite unique index: one notification per show+episode combo per user.
	// GORM creates a single index spanning all three fields tagged with the same index name.
	UserID     uint   `gorm:"uniqueIndex:idx_user_episode"`
	ShowTitle  string `gorm:"uniqueIndex:idx_user_episode"`
	EpisodeKey string `gorm:"uniqueIndex:idx_user_episode"` // e.g. "S02E05"
}

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

// PostgresStore is the concrete implementation of Service using GORM + PostgreSQL.
// It holds a private db field — no other package can access GORM directly.
type PostgresStore struct {
	db *gorm.DB
}

// Connect opens a GORM connection to PostgreSQL, runs auto-migration,
// and returns a *PostgresStore that satisfies the Service interface.
func Connect(databaseURL string) (*PostgresStore, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// AutoMigrate creates or updates the table schema to match the struct.
	// It will NOT delete unused columns — only add new ones or modify existing ones.
	if err := db.AutoMigrate(&User{}, &Notification{}, &Topic{}); err != nil {
		return nil, fmt.Errorf("running auto-migration: %w", err)
	}

	// &PostgresStore{db: db} creates a pointer to a new PostgresStore.
	// The &  operator takes the address — like & in C, giving you a pointer.
	return &PostgresStore{db: db}, nil
}

// GetAllUsers returns every user in the database.
// db.Find populates the slice and returns a *gorm.DB result —
// we check result.Error to see if anything went wrong.
func (s *PostgresStore) GetAllUsers() ([]User, error) {
	var users []User
	result := s.db.Find(&users)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching all users: %w", result.Error)
	}
	return users, nil
}

// GetUserByTelegramID looks up a user by their Telegram ID.
// Returns (nil, nil) if the user doesn't exist — the caller checks for nil
// to distinguish "not found" from "database error".
func (s *PostgresStore) GetUserByTelegramID(telegramID int64) (*User, error) {
	var user User
	err := s.db.Where("telegram_id = ?", telegramID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching user by telegram ID %d: %w", telegramID, err)
	}
	return &user, nil
}

// CreateUser inserts a new user record into the database.
func (s *PostgresStore) CreateUser(user *User) error {
	result := s.db.Create(user)
	if result.Error != nil {
		return fmt.Errorf("creating user: %w", result.Error)
	}
	return nil
}

// UpdateUserChatID changes the ChatID for an existing user, moving their
// notifications to a different chat without touching their Trakt tokens.
func (s *PostgresStore) UpdateUserChatID(telegramID, chatID int64) error {
	result := s.db.Model(&User{}).Where("telegram_id = ?", telegramID).Update("chat_id", chatID)
	if result.Error != nil {
		return fmt.Errorf("updating chat ID for user %d: %w", telegramID, result.Error)
	}
	return nil
}

func (s *PostgresStore) UpdateUserNames(telegramID int64, firstName, username string) error {
	result := s.db.Model(&User{}).Where("telegram_id = ?", telegramID).Updates(User{FirstName: firstName, Username: username})
	if result.Error != nil {
		return fmt.Errorf("updating names for user %d: %w", telegramID, result.Error)
	}
	return nil
}

// CreateOrUpdateUser upserts a user by TelegramID — updates tokens and ChatID
// if the user already exists, otherwise inserts a new record.
func (s *PostgresStore) CreateOrUpdateUser(user *User) error {
	result := s.db.
		Where("telegram_id = ?", user.TelegramID).
		Assign(User{
			FirstName:         user.FirstName,
			Username:          user.Username,
			ChatID:            user.ChatID,
			TraktAccessToken:  user.TraktAccessToken,
			TraktRefreshToken: user.TraktRefreshToken,
		}).
		FirstOrCreate(user)
	if result.Error != nil {
		return fmt.Errorf("upserting user: %w", result.Error)
	}
	return nil
}

// HasNotification checks whether a notification already exists for the given
// userID, showTitle, and episodeKey combination.
func (s *PostgresStore) HasNotification(userID uint, showTitle, episodeKey string) (bool, error) {
	var notification Notification
	err := s.db.Where(
		"user_id = ? AND show_title = ? AND episode_key = ?",
		userID, showTitle, episodeKey,
	).First(&notification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return true, err
}

// CreateNotification inserts a new notification record into the database.
func (s *PostgresStore) CreateNotification(notification *Notification) error {
	result := s.db.Create(notification)
	if result.Error != nil {
		return fmt.Errorf("creating notification: %w", result.Error)
	}
	return nil
}

// HasUserInChat checks whether at least one authenticated user exists
// for the given chat. Uses the same errors.Is pattern as HasNotification —
// ErrRecordNotFound means no user, any other error is a real failure.
func (s *PostgresStore) HasUserInChat(chatID int64) (bool, error) {
	var user User
	err := s.db.Where("chat_id = ?", chatID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking users in chat %d: %w", chatID, err)
	}
	return true, nil
}

// GetTopics returns all registered forum topics for a given chat.
func (s *PostgresStore) GetTopics(chatID int64) ([]Topic, error) {
	var topics []Topic
	result := s.db.Where("chat_id = ?", chatID).Find(&topics)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching topics for chat %d: %w", chatID, result.Error)
	}
	return topics, nil
}

// CreateOrUpdateTopic upserts a topic — if the chat+thread combo already exists,
// it updates the name. Otherwise it creates a new record.
// This uses GORM's Assign + FirstOrCreate pattern:
//   - Where clause finds the row by ChatID+ThreadID
//   - Assign sets the fields to update if it already exists
//   - FirstOrCreate either finds or inserts
func (s *PostgresStore) CreateOrUpdateTopic(topic *Topic) error {
	result := s.db.
		Where("chat_id = ? AND thread_id = ?", topic.ChatID, topic.ThreadID).
		Assign(Topic{Name: topic.Name}).
		FirstOrCreate(topic)
	if result.Error != nil {
		return fmt.Errorf("upserting topic: %w", result.Error)
	}
	return nil
}

// UpdateUserMuted sets the Muted flag for a user, controlling whether
// they receive episode notifications.
func (s *PostgresStore) UpdateUserMuted(telegramID int64, muted bool) error {
	result := s.db.Model(&User{}).Where("telegram_id = ?", telegramID).Update("muted", muted)
	if result.Error != nil {
		return fmt.Errorf("updating muted status for user %d: %w", telegramID, result.Error)
	}
	return nil
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
