// Package mocks provides shared test doubles for the storage and trakt interfaces.
// These are NOT _test.go files — they're importable by any package's tests.
// Types are exported (uppercase) so they're visible outside this package.
package mocks

import (
	"context"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/mock"
)

// MockStore is a testify mock for storage.Service.
// Usage: store := &mocks.MockStore{}
//
//	store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
type MockStore struct {
	mock.Mock
}

func (m *MockStore) GetUserByTelegramID(ctx context.Context, telegramID int64) (*storage.User, error) {
	args := m.Called(ctx, telegramID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.User), args.Error(1)
}

func (m *MockStore) GetUserByUsername(ctx context.Context, username string) (*storage.User, error) {
	args := m.Called(ctx, username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.User), args.Error(1)
}

func (m *MockStore) GetNotificationByID(ctx context.Context, id uint) (*storage.Notification, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.Notification), args.Error(1)
}

func (m *MockStore) GetNotificationByMessageID(ctx context.Context, messageID int) (*storage.Notification, error) {
	args := m.Called(ctx, messageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.Notification), args.Error(1)
}

func (m *MockStore) CreateUser(ctx context.Context, user *storage.User) error {
	return m.Called(ctx, user).Error(0)
}

func (m *MockStore) CreateOrUpdateUser(ctx context.Context, user *storage.User) error {
	return m.Called(ctx, user).Error(0)
}

func (m *MockStore) UpdateUserChatID(ctx context.Context, telegramID, chatID int64) error {
	return m.Called(ctx, telegramID, chatID).Error(0)
}

func (m *MockStore) UpdateUserNames(ctx context.Context, telegramID int64, firstName, username string) error {
	return m.Called(ctx, telegramID, firstName, username).Error(0)
}

func (m *MockStore) UpdateNotificationMessageID(ctx context.Context, notificationID uint, messageID int) error {
	return m.Called(ctx, notificationID, messageID).Error(0)
}

func (m *MockStore) HasNotification(ctx context.Context, chatID int64, showTitle string, season, episodeNumber int) (bool, error) {
	args := m.Called(ctx, chatID, showTitle, season, episodeNumber)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) CreateNotification(ctx context.Context, notification *storage.Notification) error {
	return m.Called(ctx, notification).Error(0)
}

func (m *MockStore) HasUserInChat(ctx context.Context, chatID int64) (bool, error) {
	args := m.Called(ctx, chatID)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) GetTopics(ctx context.Context, chatID int64) ([]storage.Topic, error) {
	args := m.Called(ctx, chatID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.Topic), args.Error(1)
}

func (m *MockStore) CreateOrUpdateTopic(ctx context.Context, topic *storage.Topic) error {
	return m.Called(ctx, topic).Error(0)
}

func (m *MockStore) UpdateUserTokens(ctx context.Context, telegramID int64, accessToken, refreshToken string, expiresAt time.Time) error {
	return m.Called(ctx, telegramID, accessToken, refreshToken, expiresAt).Error(0)
}

func (m *MockStore) UpdateUserMuted(ctx context.Context, telegramID int64, muted bool) error {
	return m.Called(ctx, telegramID, muted).Error(0)
}

func (m *MockStore) GetDistinctChatIDs(ctx context.Context) ([]int64, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]int64), args.Error(1)
}

func (m *MockStore) GetUsersByChatID(ctx context.Context, chatID int64) ([]storage.User, error) {
	args := m.Called(ctx, chatID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.User), args.Error(1)
}

func (m *MockStore) CreateWatchStatuses(ctx context.Context, notificationID uint, userIDs []uint) error {
	return m.Called(ctx, notificationID, userIDs).Error(0)
}

func (m *MockStore) GetWatchStatuses(ctx context.Context, notificationID uint) ([]storage.WatchStatus, error) {
	args := m.Called(ctx, notificationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.WatchStatus), args.Error(1)
}

func (m *MockStore) GetUserWatchStatus(ctx context.Context, notificationType storage.NotificationType, notificationID uint, userID uint) (storage.WatchStatus, error) {
	args := m.Called(ctx, notificationType, notificationID, userID)
	return args.Get(0).(storage.WatchStatus), args.Error(1)
}

func (m *MockStore) GetUnwatchedStatusesByUser(ctx context.Context, userID uint) ([]storage.WatchStatus, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.WatchStatus), args.Error(1)
}

func (m *MockStore) MarkWatchStatus(ctx context.Context, notificationType storage.NotificationType, notificationID uint, userID uint) error {
	return m.Called(ctx, notificationType, notificationID, userID).Error(0)
}

func (m *MockStore) UnmarkWatchStatus(ctx context.Context, notificationType storage.NotificationType, notificationID uint, userID uint) error {
	return m.Called(ctx, notificationType, notificationID, userID).Error(0)
}

func (m *MockStore) GetChatConfig(ctx context.Context, chatID int64) (*storage.ChatConfig, error) {
	args := m.Called(ctx, chatID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.ChatConfig), args.Error(1)
}

func (m *MockStore) CreateOrUpdateChatConfig(ctx context.Context, config *storage.ChatConfig) error {
	return m.Called(ctx, config).Error(0)
}

func (m *MockStore) CreateScheduledDeletion(ctx context.Context, deletion *storage.ScheduledDeletion) error {
	return m.Called(ctx, deletion).Error(0)
}

func (m *MockStore) GetPendingDeletions(ctx context.Context) ([]storage.ScheduledDeletion, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.ScheduledDeletion), args.Error(1)
}

func (m *MockStore) RemoveScheduledDeletion(ctx context.Context, id uint) error {
	return m.Called(ctx, id).Error(0)
}

func (m *MockStore) GetMovieSubscription(ctx context.Context, userID uint, subType string) (*storage.MovieSubscription, error) {
	args := m.Called(ctx, userID, subType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.MovieSubscription), args.Error(1)
}

func (m *MockStore) CreateMovieSubscription(ctx context.Context, sub *storage.MovieSubscription) error {
	return m.Called(ctx, sub).Error(0)
}

func (m *MockStore) DeleteMovieSubscription(ctx context.Context, userID uint, subType string) error {
	return m.Called(ctx, userID, subType).Error(0)
}

func (m *MockStore) GetMovieSubscribers(ctx context.Context) ([]storage.MovieSubscription, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.MovieSubscription), args.Error(1)
}

func (m *MockStore) CreateFollowedMovie(ctx context.Context, fm *storage.FollowedMovie) error {
	return m.Called(ctx, fm).Error(0)
}

func (m *MockStore) HasFollowedMovie(ctx context.Context, userID uint, traktMovieID int) (bool, error) {
	args := m.Called(ctx, userID, traktMovieID)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) CreateMovieNotification(ctx context.Context, mn *storage.MovieNotification) error {
	return m.Called(ctx, mn).Error(0)
}

func (m *MockStore) GetMovieNotificationByID(ctx context.Context, id uint) (*storage.MovieNotification, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.MovieNotification), args.Error(1)
}

func (m *MockStore) HasMovieNotification(ctx context.Context, chatID int64, traktMovieID int) (bool, error) {
	args := m.Called(ctx, chatID, traktMovieID)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) UpdateMovieNotificationMessageID(ctx context.Context, id uint, messageID int) error {
	return m.Called(ctx, id, messageID).Error(0)
}

func (m *MockStore) CreateWatchStatusesWithType(ctx context.Context, notificationType storage.NotificationType, notificationID uint, userIDs []uint) error {
	return m.Called(ctx, notificationType, notificationID, userIDs).Error(0)
}

func (m *MockStore) GetWatchStatusesByType(ctx context.Context, notificationType storage.NotificationType, notificationID uint) ([]storage.WatchStatus, error) {
	args := m.Called(ctx, notificationType, notificationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.WatchStatus), args.Error(1)
}
