package storage

import (
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgresStore is the concrete implementation of Service using GORM + PostgreSQL.
// It holds a private db field — no other package can access GORM directly.
type PostgresStore struct {
	db *gorm.DB
}

// Connect opens a GORM connection to PostgreSQL, runs auto-migration,
// and returns a *PostgresStore that satisfies the Service interface.
func Connect(databaseURL string) (*PostgresStore, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		// Suppress "record not found" warnings — these are expected when checking
		// if a record exists (e.g. HasNotification, GetUserByTelegramID).
		Logger: logger.New(log.Default(), logger.Config{
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// AutoMigrate creates or updates the table schema to match the struct.
	// It will NOT delete unused columns — only add new ones or modify existing ones.
	if err := db.AutoMigrate(&User{}, &Notification{}, &Topic{}, &WatchStatus{}, &ScheduledDeletion{}); err != nil {
		return nil, fmt.Errorf("running auto-migration: %w", err)
	}

	// &PostgresStore{db: db} creates a pointer to a new PostgresStore.
	// The &  operator takes the address — like & in C, giving you a pointer.
	return &PostgresStore{db: db}, nil
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

// GetNotificationByID looks up a notification by its database primary key.
func (s *PostgresStore) GetNotificationByID(id uint) (*Notification, error) {
	var notification Notification
	err := s.db.First(&notification, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching notification by ID %d: %w", id, err)
	}
	return &notification, nil
}

func (s *PostgresStore) GetNotificationByMessageID(messageID int) (*Notification, error) {
	var notification Notification
	err := s.db.Where("telegram_message_id = ?", messageID).First(&notification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching notification by message ID %d: %w", messageID, err)
	}
	return &notification, nil
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

func (s *PostgresStore) UpdateNotificationMessageID(notificationID uint, messageID int) error {
	result := s.db.Model(&Notification{}).Where("id = ?", notificationID).Update("telegram_message_id", messageID)
	if result.Error != nil {
		return fmt.Errorf("updating notification message ID for ID %d: %w", notificationID, result.Error)
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
// chatID, showTitle, season, and episodeNumber combination.
func (s *PostgresStore) HasNotification(chatID int64, showTitle string, season, episodeNumber int) (bool, error) {
	var notification Notification
	err := s.db.Where(
		"chat_id = ? AND show_title = ? AND season = ? AND episode_number = ?",
		chatID, showTitle, season, episodeNumber,
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

// GetDistinctChatIDs returns the unique chat IDs that have at least one active
// (non-muted) user. Used to drive per-chat episode checking instead of per-user.
// Model(&User{}) targets the users table, Distinct+Pluck extracts a flat []int64
// rather than full User structs — like SELECT DISTINCT chat_id FROM users WHERE ...
func (s *PostgresStore) GetDistinctChatIDs() ([]int64, error) {
	var chatIDs []int64
	result := s.db.Model(&User{}).Distinct("chat_id").Where("muted = ?", false).Pluck("chat_id", &chatIDs)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching distinct chat IDs: %w", result.Error)
	}
	return chatIDs, nil
}

// GetUsersByChatID returns all active (non-muted) users for a given chat.
// Returns full User structs because callers need the Trakt tokens to make API calls.
func (s *PostgresStore) GetUsersByChatID(chatID int64) ([]User, error) {
	var users []User
	result := s.db.Where("chat_id = ? AND muted = ?", chatID, false).Find(&users)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching users for chat ID %d: %w", chatID, result.Error)
	}
	return users, nil
}

// CreateWatchStatuses bulk-inserts WatchStatus rows for a notification — one per user.
// All start with Watched=false (⏳). Uses a slice of user IDs rather than full User
// structs because that's all the caller needs to pass. The loop builds a []WatchStatus
// slice, then Create() inserts them all in a single SQL INSERT.
func (s *PostgresStore) CreateWatchStatuses(notificationID uint, userIDs []uint) error {
	statuses := make([]WatchStatus, len(userIDs))
	for i, uid := range userIDs {
		statuses[i] = WatchStatus{
			NotificationID: notificationID,
			UserID:         uid,
			Watched:        false,
		}
	}
	if err := s.db.Create(&statuses).Error; err != nil {
		return fmt.Errorf("creating watch statuses for notification %d: %w", notificationID, err)
	}
	return nil
}

// GetWatchStatuses returns all WatchStatus rows for a notification, with each
// row's User pre-loaded so callers can access usernames/names for display.
// Preload("User") tells GORM to run a second query to fill the User field —
// similar to SQLAlchemy's joinedload() or Entity Framework's Include().
func (s *PostgresStore) GetWatchStatuses(notificationID uint) ([]WatchStatus, error) {
	var statuses []WatchStatus
	err := s.db.Preload("User").Where("notification_id = ?", notificationID).Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching watch statuses for notification %d: %w", notificationID, err)
	}
	return statuses, nil
}

func (s *PostgresStore) GetUserWatchStatus(notificationID uint, userID uint) (WatchStatus, error) {
	var status WatchStatus
	err := s.db.Where("notification_id = ? AND user_id = ?", notificationID, userID).First(&status).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status, nil
		}
		return status, fmt.Errorf("fetching watch status for notification %d and user %d: %w", notificationID, userID, err)
	}
	return status, nil
}

// GetUnwatchedStatusesByUser returns all WatchStatus rows where a specific user
// hasn't watched yet (Watched=false), with the Notification preloaded.
// This lets the caller access notification.TraktShowID, Season, EpisodeNumber
// to cross-reference against Trakt watch history.
func (s *PostgresStore) GetUnwatchedStatusesByUser(userID uint) ([]WatchStatus, error) {
	var statuses []WatchStatus
	err := s.db.Preload("Notification").
		Where("user_id = ? AND watched = ?", userID, false).
		Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching unwatched statuses for user %d: %w", userID, err)
	}
	return statuses, nil
}

// MarkWatchStatus sets Watched=true for a specific user on a specific notification.
// Uses GORM's Update with a Where clause targeting both foreign keys.
func (s *PostgresStore) MarkWatchStatus(notificationID uint, userID uint) error {
	result := s.db.Model(&WatchStatus{}).
		Where("notification_id = ? AND user_id = ?", notificationID, userID).
		Update("watched", true)
	if result.Error != nil {
		return fmt.Errorf("marking watch status for notification %d, user %d: %w", notificationID, userID, result.Error)
	}
	return nil
}

// CreateScheduledDeletion inserts a new pending deletion record.
func (s *PostgresStore) CreateScheduledDeletion(deletion *ScheduledDeletion) error {
	if err := s.db.Create(deletion).Error; err != nil {
		return fmt.Errorf("creating scheduled deletion for notification %d: %w", deletion.NotificationID, err)
	}
	return nil
}

// GetPendingDeletions returns all scheduled deletions whose DeleteAt time has passed.
// time.Now() is evaluated at query time — GORM sends it as a parameter to PostgreSQL.
func (s *PostgresStore) GetPendingDeletions() ([]ScheduledDeletion, error) {
	var deletions []ScheduledDeletion
	err := s.db.Where("delete_at <= ?", time.Now()).Find(&deletions).Error
	if err != nil {
		return nil, fmt.Errorf("fetching pending deletions: %w", err)
	}
	return deletions, nil
}

// RemoveScheduledDeletion hard-deletes a processed deletion record by ID.
// Uses Unscoped() to bypass GORM's soft-delete — we don't need to keep these around.
func (s *PostgresStore) RemoveScheduledDeletion(id uint) error {
	if err := s.db.Unscoped().Delete(&ScheduledDeletion{}, id).Error; err != nil {
		return fmt.Errorf("removing scheduled deletion %d: %w", id, err)
	}
	return nil
}
