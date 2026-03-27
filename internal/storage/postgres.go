package storage

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// User represents a Telegram user who has linked their Trakt.tv account.
// GORM maps this struct to a "users" table automatically.
type User struct {
	gorm.Model        // embeds ID, CreatedAt, UpdatedAt, DeletedAt
	TelegramID        int64  `gorm:"uniqueIndex"`
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

// Connect opens a GORM connection to PostgreSQL and runs auto-migration
// for all models. Returns the database handle or an error.
func Connect(databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// AutoMigrate creates or updates the table schema to match the struct.
	// It will NOT delete unused columns — only add new ones or modify existing ones.
	if err := db.AutoMigrate(&User{}, &Notification{}); err != nil {
		return nil, fmt.Errorf("running auto-migration: %w", err)
	}

	return db, nil
}
