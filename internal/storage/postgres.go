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
	ChatID            int64 // Telegram chat where this user authenticated
	TraktAccessToken  string
	TraktRefreshToken string
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
	CreateUser(user *User) error
	HasNotification(userID uint, showTitle, episodeKey string) (bool, error)
	CreateNotification(notification *Notification) error
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
	if err := db.AutoMigrate(&User{}, &Notification{}); err != nil {
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

// CreateUser inserts a new user record into the database.
func (s *PostgresStore) CreateUser(user *User) error {
	result := s.db.Create(user)
	if result.Error != nil {
		return fmt.Errorf("creating user: %w", result.Error)
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
