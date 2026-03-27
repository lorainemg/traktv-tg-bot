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

// Connect opens a GORM connection to PostgreSQL and runs auto-migration
// for all models. Returns the database handle or an error.
func Connect(databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// AutoMigrate creates or updates the table schema to match the struct.
	// It will NOT delete unused columns — only add new ones or modify existing ones.
	err = db.AutoMigrate(&User{})
	if err != nil {
		return nil, fmt.Errorf("running auto-migration: %w", err)
	}

	return db, nil
}
