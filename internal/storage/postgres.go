package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

// PostgresStore is the concrete implementation of Service using GORM + PostgreSQL.
// It holds a private db field - no other package can access GORM directly.
type PostgresStore struct {
	db *gorm.DB
}

// Connect opens a GORM connection to PostgreSQL, runs auto-migration,
// and returns a *PostgresStore that satisfies the Service interface.
func Connect(databaseURL string) (*PostgresStore, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		// Suppress "record not found" warnings - these are expected when checking
		// if a record exists (e.g. HasNotification, GetUserByTelegramID).
		Logger: logger.New(log.Default(), logger.Config{
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := db.Use(tracing.NewPlugin()); err != nil {
		return nil, fmt.Errorf("enabling gorm telemetry: %w", err)
	}

	// Drop the old unique index before AutoMigrate creates the new one.
	// This is needed because we're changing WatchStatus's unique index from
	// (NotificationID, UserID) to (NotificationType, NotificationID, UserID).
	// GORM AutoMigrate won't drop the old index automatically.
	if db.Migrator().HasIndex(&WatchStatus{}, "idx_notification_user") {
		if err := db.Migrator().DropIndex(&WatchStatus{}, "idx_notification_user"); err != nil {
			return nil, fmt.Errorf("dropping old watch status index: %w", err)
		}
	}

	// Drop the old ScheduledDeletion unique index on just NotificationID.
	// The old index name depends on whether it had a custom name or GORM auto-generated it.
	for _, oldIdx := range []string{"idx_notification_id", "idx_scheduled_deletions_notification_id"} {
		if db.Migrator().HasIndex(&ScheduledDeletion{}, oldIdx) {
			if err := db.Migrator().DropIndex(&ScheduledDeletion{}, oldIdx); err != nil {
				return nil, fmt.Errorf("dropping old scheduled deletion index %s: %w", oldIdx, err)
			}
			break
		}
	}

	// AutoMigrate creates or updates the table schema to match the struct.
	// It will NOT delete unused columns - only add new ones or modify existing ones.
	if err := db.AutoMigrate(
		&User{}, &Notification{}, &Topic{}, &WatchStatus{}, &ScheduledDeletion{}, &ChatConfig{},
		&MovieSubscription{}, &FollowedMovie{}, &MovieNotification{},
	); err != nil {
		return nil, fmt.Errorf("running auto-migration: %w", err)
	}

	// Backfill: existing WatchStatus rows were created before NotificationType existed.
	// GORM sets new string columns to "" (empty string) for existing rows.
	// Set them all to "episode" since that's the only type that existed before.
	db.Exec("UPDATE watch_statuses SET notification_type = ? WHERE notification_type = '' OR notification_type IS NULL", NotificationEpisode)

	// Backfill: existing ScheduledDeletion rows are all for episodes.
	db.Exec("UPDATE scheduled_deletions SET notification_type = ? WHERE notification_type = '' OR notification_type IS NULL", NotificationEpisode)

	// &PostgresStore{db: db} creates a pointer to a new PostgresStore.
	// The &  operator takes the address - like & in C, giving you a pointer.
	return &PostgresStore{db: db}, nil
}

// GetUserByTelegramID looks up a user by their Telegram ID.
// Returns (nil, nil) if the user doesn't exist - the caller checks for nil
// to distinguish "not found" from "database error".
func (s *PostgresStore) GetUserByTelegramID(ctx context.Context, telegramID int64) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("telegram_id = ?", telegramID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching user by telegram ID %d: %w", telegramID, err)
	}
	return &user, nil
}

// GetUserByUsername looks up a user by their Telegram username (case-insensitive).
// Returns (nil, nil) if the user doesn't exist — same pattern as GetUserByTelegramID.
func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("LOWER(username) = LOWER(?)", username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching user by username %q: %w", username, err)
	}
	return &user, nil
}

// GetNotificationByID looks up a notification by its database primary key.
func (s *PostgresStore) GetNotificationByID(ctx context.Context, id uint) (*Notification, error) {
	var notification Notification
	err := s.db.WithContext(ctx).First(&notification, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching notification by ID %d: %w", id, err)
	}
	return &notification, nil
}

func (s *PostgresStore) GetNotificationByMessageID(ctx context.Context, messageID int) (*Notification, error) {
	var notification Notification
	err := s.db.WithContext(ctx).Where("telegram_message_id = ?", messageID).First(&notification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching notification by message ID %d: %w", messageID, err)
	}
	return &notification, nil
}

// CreateUser inserts a new user record into the database.
func (s *PostgresStore) CreateUser(ctx context.Context, user *User) error {
	result := s.db.WithContext(ctx).Create(user)
	if result.Error != nil {
		return fmt.Errorf("creating user: %w", result.Error)
	}
	return nil
}

// UpdateUserChatID changes the ChatID for an existing user, moving their
// notifications to a different chat without touching their Trakt tokens.
func (s *PostgresStore) UpdateUserChatID(ctx context.Context, telegramID, chatID int64) error {
	result := s.db.WithContext(ctx).Model(&User{}).Where("telegram_id = ?", telegramID).Update("chat_id", chatID)
	if result.Error != nil {
		return fmt.Errorf("updating chat ID for user %d: %w", telegramID, result.Error)
	}
	return nil
}

func (s *PostgresStore) UpdateUserNames(ctx context.Context, telegramID int64, firstName, username string) error {
	result := s.db.WithContext(ctx).Model(&User{}).Where("telegram_id = ?", telegramID).Updates(User{FirstName: firstName, Username: username})
	if result.Error != nil {
		return fmt.Errorf("updating names for user %d: %w", telegramID, result.Error)
	}
	return nil
}

func (s *PostgresStore) UpdateNotificationMessageID(ctx context.Context, notificationID uint, messageID int) error {
	result := s.db.WithContext(ctx).Model(&Notification{}).Where("id = ?", notificationID).Update("telegram_message_id", messageID)
	if result.Error != nil {
		return fmt.Errorf("updating notification message ID for ID %d: %w", notificationID, result.Error)
	}
	return nil
}

// CreateOrUpdateUser upserts a user by TelegramID - updates tokens and ChatID
// if the user already exists, otherwise inserts a new record.
func (s *PostgresStore) CreateOrUpdateUser(ctx context.Context, user *User) error {
	result := s.db.WithContext(ctx).
		Where("telegram_id = ?", user.TelegramID).
		Assign(User{
			FirstName:           user.FirstName,
			Username:            user.Username,
			ChatID:              user.ChatID,
			TraktAccessToken:    user.TraktAccessToken,
			TraktRefreshToken:   user.TraktRefreshToken,
			TraktTokenExpiresAt: user.TraktTokenExpiresAt,
		}).
		FirstOrCreate(user)
	if result.Error != nil {
		return fmt.Errorf("upserting user: %w", result.Error)
	}
	return nil
}

// HasNotification checks whether a notification already exists for the given
// chatID, showTitle, season, and episodeNumber combination.
func (s *PostgresStore) HasNotification(ctx context.Context, chatID int64, showTitle string, season, episodeNumber int) (bool, error) {
	var notification Notification
	err := s.db.WithContext(ctx).Where(
		"chat_id = ? AND show_title = ? AND season = ? AND episode_number = ?",
		chatID, showTitle, season, episodeNumber,
	).First(&notification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return true, err
}

// CreateNotification inserts a new notification record into the database.
func (s *PostgresStore) CreateNotification(ctx context.Context, notification *Notification) error {
	result := s.db.WithContext(ctx).Create(notification)
	if result.Error != nil {
		return fmt.Errorf("creating notification: %w", result.Error)
	}
	return nil
}

// HasUserInChat checks whether at least one authenticated user exists
// for the given chat. Uses the same errors.Is pattern as HasNotification -
// ErrRecordNotFound means no user, any other error is a real failure.
func (s *PostgresStore) HasUserInChat(ctx context.Context, chatID int64) (bool, error) {
	var user User
	err := s.db.WithContext(ctx).Where("chat_id = ?", chatID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking users in chat %d: %w", chatID, err)
	}
	return true, nil
}

// GetTopics returns all registered forum topics for a given chat.
func (s *PostgresStore) GetTopics(ctx context.Context, chatID int64) ([]Topic, error) {
	var topics []Topic
	result := s.db.WithContext(ctx).Where("chat_id = ?", chatID).Find(&topics)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching topics for chat %d: %w", chatID, result.Error)
	}
	return topics, nil
}

// CreateOrUpdateTopic upserts a topic - if the chat+thread combo already exists,
// it updates the name. Otherwise it creates a new record.
// This uses GORM's Assign + FirstOrCreate pattern:
//   - Where clause finds the row by ChatID+ThreadID
//   - Assign sets the fields to update if it already exists
//   - FirstOrCreate either finds or inserts
func (s *PostgresStore) CreateOrUpdateTopic(ctx context.Context, topic *Topic) error {
	result := s.db.WithContext(ctx).
		Where("chat_id = ? AND thread_id = ?", topic.ChatID, topic.ThreadID).
		Assign(Topic{Name: topic.Name}).
		FirstOrCreate(topic)
	if result.Error != nil {
		return fmt.Errorf("upserting topic: %w", result.Error)
	}
	return nil
}

// UpdateUserTokens saves a fresh access/refresh token pair and expiry time.
// Called after a successful token refresh to persist the new credentials.
func (s *PostgresStore) UpdateUserTokens(ctx context.Context, telegramID int64, accessToken, refreshToken string, expiresAt time.Time) error {
	result := s.db.WithContext(ctx).Model(&User{}).Where("telegram_id = ?", telegramID).Updates(map[string]any{
		"trakt_access_token":     accessToken,
		"trakt_refresh_token":    refreshToken,
		"trakt_token_expires_at": expiresAt,
	})
	if result.Error != nil {
		return fmt.Errorf("updating tokens for user %d: %w", telegramID, result.Error)
	}
	return nil
}

// UpdateUserMuted sets the Muted flag for a user, controlling whether
// they receive episode notifications.
func (s *PostgresStore) UpdateUserMuted(ctx context.Context, telegramID int64, muted bool) error {
	result := s.db.WithContext(ctx).Model(&User{}).Where("telegram_id = ?", telegramID).Update("muted", muted)
	if result.Error != nil {
		return fmt.Errorf("updating muted status for user %d: %w", telegramID, result.Error)
	}
	return nil
}

// GetDistinctChatIDs returns the unique chat IDs that have at least one active
// (non-muted) user. Used to drive per-chat episode checking instead of per-user.
// Model(&User{}) targets the users table, Distinct+Pluck extracts a flat []int64
// rather than full User structs - like SELECT DISTINCT chat_id FROM users WHERE ...
func (s *PostgresStore) GetDistinctChatIDs(ctx context.Context) ([]int64, error) {
	var chatIDs []int64
	result := s.db.WithContext(ctx).Model(&User{}).Distinct("chat_id").Where("muted = ?", false).Pluck("chat_id", &chatIDs)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching distinct chat IDs: %w", result.Error)
	}
	return chatIDs, nil
}

// GetUsersByChatID returns all active (non-muted) users for a given chat.
// Returns full User structs because callers need the Trakt tokens to make API calls.
func (s *PostgresStore) GetUsersByChatID(ctx context.Context, chatID int64) ([]User, error) {
	var users []User
	result := s.db.WithContext(ctx).Where("chat_id = ? AND muted = ?", chatID, false).Find(&users)
	if result.Error != nil {
		return nil, fmt.Errorf("fetching users for chat ID %d: %w", chatID, result.Error)
	}
	return users, nil
}

// CreateWatchStatuses bulk-inserts WatchStatus rows for a notification - one per user.
// All start with Watched=false (⏳). Uses a slice of user IDs rather than full User
// structs because that's all the caller needs to pass. The loop builds a []WatchStatus
// slice, then Create() inserts them all in a single SQL INSERT.
func (s *PostgresStore) CreateWatchStatuses(ctx context.Context, notificationID uint, userIDs []uint) error {
	statuses := make([]WatchStatus, len(userIDs))
	for i, uid := range userIDs {
		statuses[i] = WatchStatus{
			NotificationID: notificationID,
			UserID:         uid,
			Watched:        false,
		}
	}
	if err := s.db.WithContext(ctx).Create(&statuses).Error; err != nil {
		return fmt.Errorf("creating watch statuses for notification %d: %w", notificationID, err)
	}
	return nil
}

// GetWatchStatuses returns all WatchStatus rows for a notification, with each
// row's User pre-loaded so callers can access usernames/names for display.
// Preload("User") tells GORM to run a second query to fill the User field -
// similar to SQLAlchemy's joinedload() or Entity Framework's Include().
func (s *PostgresStore) GetWatchStatuses(ctx context.Context, notificationID uint) ([]WatchStatus, error) {
	var statuses []WatchStatus
	err := s.db.WithContext(ctx).Preload("User").Where("notification_id = ?", notificationID).Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching watch statuses for notification %d: %w", notificationID, err)
	}
	return statuses, nil
}

func (s *PostgresStore) GetUserWatchStatus(ctx context.Context, notificationType NotificationType, notificationID uint, userID uint) (WatchStatus, error) {
	var status WatchStatus
	err := s.db.WithContext(ctx).Where("notification_type = ? AND notification_id = ? AND user_id = ?", notificationType, notificationID, userID).First(&status).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status, nil
		}
		return status, fmt.Errorf("fetching watch status for %s notification %d and user %d: %w", notificationType, notificationID, userID, err)
	}
	return status, nil
}

// GetUnwatchedStatusesByUser returns all WatchStatus rows where a specific user
// hasn't watched yet (Watched=false), with the Notification preloaded.
// This lets the caller access notification.TraktShowID, Season, EpisodeNumber
// to cross-reference against Trakt watch history.
func (s *PostgresStore) GetUnwatchedStatusesByUser(ctx context.Context, userID uint) ([]WatchStatus, error) {
	var statuses []WatchStatus
	err := s.db.WithContext(ctx).Preload("Notification").
		Where("user_id = ? AND watched = ?", userID, false).
		Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching unwatched statuses for user %d: %w", userID, err)
	}
	return statuses, nil
}

// MarkWatchStatus sets Watched=true for a specific user on a specific notification.
// Uses GORM's Update with a Where clause targeting both foreign keys.
func (s *PostgresStore) MarkWatchStatus(ctx context.Context, notificationType NotificationType, notificationID uint, userID uint) error {
	result := s.db.WithContext(ctx).Model(&WatchStatus{}).
		Where("notification_type = ? AND notification_id = ? AND user_id = ?", notificationType, notificationID, userID).
		Update("watched", true)
	if result.Error != nil {
		return fmt.Errorf("marking watch status for %s notification %d, user %d: %w", notificationType, notificationID, userID, result.Error)
	}
	return nil
}

// UnmarkWatchStatus sets Watched=false for a specific user on a specific notification.
// The reverse of MarkWatchStatus — used when a user clicks "Mark as Unwatched".
func (s *PostgresStore) UnmarkWatchStatus(ctx context.Context, notificationType NotificationType, notificationID uint, userID uint) error {
	result := s.db.WithContext(ctx).Model(&WatchStatus{}).
		Where("notification_type = ? AND notification_id = ? AND user_id = ?", notificationType, notificationID, userID).
		Update("watched", false)
	if result.Error != nil {
		return fmt.Errorf("unmarking watch status for %s notification %d, user %d: %w", notificationType, notificationID, userID, result.Error)
	}
	return nil
}

// GetChatConfig retrieves the configuration for a chat.
// Returns (nil, nil) if no config exists yet - the caller uses defaults.
func (s *PostgresStore) GetChatConfig(ctx context.Context, chatID int64) (*ChatConfig, error) {
	var config ChatConfig
	err := s.db.WithContext(ctx).Where("chat_id = ?", chatID).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching chat config for chat %d: %w", chatID, err)
	}
	return &config, nil
}

// CreateOrUpdateChatConfig upserts a chat's config by ChatID.
// If a row exists, it updates country/timezone/delete_watched; otherwise it creates one.
// Uses map[string]any instead of a struct in Assign - GORM skips zero-value struct
// fields (false, "", 0), so DeleteWatched=false would be silently ignored with a struct.
// A map explicitly includes every key, so GORM always sends the UPDATE.
func (s *PostgresStore) CreateOrUpdateChatConfig(ctx context.Context, config *ChatConfig) error {
	result := s.db.WithContext(ctx).
		Where("chat_id = ?", config.ChatID).
		Assign(map[string]any{
			"country":        config.Country,
			"timezone":       config.Timezone,
			"delete_watched": config.DeleteWatched,
			"notify_hours":   config.NotifyHours,
		}).
		FirstOrCreate(config)
	if result.Error != nil {
		return fmt.Errorf("upserting chat config for chat %d: %w", config.ChatID, result.Error)
	}
	return nil
}

// CreateScheduledDeletion inserts a new pending deletion record.
func (s *PostgresStore) CreateScheduledDeletion(ctx context.Context, deletion *ScheduledDeletion) error {
	if err := s.db.WithContext(ctx).Create(deletion).Error; err != nil {
		return fmt.Errorf("creating scheduled deletion for notification %d: %w", deletion.NotificationID, err)
	}
	return nil
}

// GetPendingDeletions returns all scheduled deletions whose DeleteAt time has passed.
// time.Now() is evaluated at query time - GORM sends it as a parameter to PostgreSQL.
func (s *PostgresStore) GetPendingDeletions(ctx context.Context) ([]ScheduledDeletion, error) {
	var deletions []ScheduledDeletion
	err := s.db.WithContext(ctx).Where("delete_at <= ?", time.Now()).Find(&deletions).Error
	if err != nil {
		return nil, fmt.Errorf("fetching pending deletions: %w", err)
	}
	return deletions, nil
}

// RemoveScheduledDeletion hard-deletes a processed deletion record by ID.
// Uses Unscoped() to bypass GORM's soft-delete - we don't need to keep these around.
func (s *PostgresStore) RemoveScheduledDeletion(ctx context.Context, id uint) error {
	if err := s.db.WithContext(ctx).Unscoped().Delete(&ScheduledDeletion{}, id).Error; err != nil {
		return fmt.Errorf("removing scheduled deletion %d: %w", id, err)
	}
	return nil
}

// --- Movie subscription methods ---

func (s *PostgresStore) GetMovieSubscription(ctx context.Context, userID uint, subType string) (*MovieSubscription, error) {
	var sub MovieSubscription
	err := s.db.WithContext(ctx).Where("user_id = ? AND type = ?", userID, subType).First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching movie subscription for user %d: %w", userID, err)
	}
	return &sub, nil
}

func (s *PostgresStore) CreateMovieSubscription(ctx context.Context, sub *MovieSubscription) error {
	if err := s.db.WithContext(ctx).Create(sub).Error; err != nil {
		return fmt.Errorf("creating movie subscription: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteMovieSubscription(ctx context.Context, userID uint, subType string) error {
	result := s.db.WithContext(ctx).Where("user_id = ? AND type = ?", userID, subType).Delete(&MovieSubscription{})
	if result.Error != nil {
		return fmt.Errorf("deleting movie subscription for user %d: %w", userID, result.Error)
	}
	return nil
}

func (s *PostgresStore) GetMovieSubscribers(ctx context.Context) ([]MovieSubscription, error) {
	var subs []MovieSubscription
	err := s.db.WithContext(ctx).Preload("User").Find(&subs).Error
	if err != nil {
		return nil, fmt.Errorf("fetching movie subscribers: %w", err)
	}
	return subs, nil
}

// --- Followed movie methods ---

func (s *PostgresStore) CreateFollowedMovie(ctx context.Context, fm *FollowedMovie) error {
	if err := s.db.WithContext(ctx).Create(fm).Error; err != nil {
		return fmt.Errorf("creating followed movie: %w", err)
	}
	return nil
}

func (s *PostgresStore) HasFollowedMovie(ctx context.Context, userID uint, traktMovieID int) (bool, error) {
	var fm FollowedMovie
	err := s.db.WithContext(ctx).Where("user_id = ? AND trakt_movie_id = ?", userID, traktMovieID).First(&fm).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking followed movie: %w", err)
	}
	return true, nil
}

// --- Movie notification methods ---

func (s *PostgresStore) CreateMovieNotification(ctx context.Context, mn *MovieNotification) error {
	if err := s.db.WithContext(ctx).Create(mn).Error; err != nil {
		return fmt.Errorf("creating movie notification: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetMovieNotificationByID(ctx context.Context, id uint) (*MovieNotification, error) {
	var mn MovieNotification
	err := s.db.WithContext(ctx).First(&mn, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching movie notification by ID %d: %w", id, err)
	}
	return &mn, nil
}

func (s *PostgresStore) HasMovieNotification(ctx context.Context, chatID int64, traktMovieID int) (bool, error) {
	var mn MovieNotification
	err := s.db.WithContext(ctx).Where("chat_id = ? AND trakt_movie_id = ?", chatID, traktMovieID).First(&mn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking movie notification: %w", err)
	}
	return true, nil
}

func (s *PostgresStore) UpdateMovieNotificationMessageID(ctx context.Context, id uint, messageID int) error {
	result := s.db.WithContext(ctx).Model(&MovieNotification{}).Where("id = ?", id).Update("telegram_message_id", messageID)
	if result.Error != nil {
		return fmt.Errorf("updating movie notification message ID for ID %d: %w", id, result.Error)
	}
	return nil
}

// --- Typed WatchStatus methods ---

// CreateWatchStatusesWithType bulk-inserts WatchStatus rows with a NotificationType.
// Used for both episode and movie notifications.
func (s *PostgresStore) CreateWatchStatusesWithType(ctx context.Context, notificationType NotificationType, notificationID uint, userIDs []uint) error {
	statuses := make([]WatchStatus, len(userIDs))
	for i, uid := range userIDs {
		statuses[i] = WatchStatus{
			NotificationType: notificationType,
			NotificationID:   notificationID,
			UserID:           uid,
			Watched:          false,
		}
	}
	if err := s.db.WithContext(ctx).Create(&statuses).Error; err != nil {
		return fmt.Errorf("creating watch statuses for %s notification %d: %w", notificationType, notificationID, err)
	}
	return nil
}

// GetWatchStatusesByType returns WatchStatus rows filtered by NotificationType.
func (s *PostgresStore) GetWatchStatusesByType(ctx context.Context, notificationType NotificationType, notificationID uint) ([]WatchStatus, error) {
	var statuses []WatchStatus
	err := s.db.WithContext(ctx).Preload("User").
		Where("notification_type = ? AND notification_id = ?", notificationType, notificationID).
		Find(&statuses).Error
	if err != nil {
		return nil, fmt.Errorf("fetching watch statuses for %s notification %d: %w", notificationType, notificationID, err)
	}
	return statuses, nil
}
