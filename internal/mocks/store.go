// Package mocks provides shared test doubles for the storage and trakt interfaces.
// These are NOT _test.go files — they're importable by any package's tests.
// Types are exported (uppercase) so they're visible outside this package.
package mocks

import (
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/mock"
)

// MockStore is a testify mock for storage.Service.
// Usage: store := &mocks.MockStore{}
//
//	store.On("GetUserByTelegramID", int64(111)).Return(user, nil)
type MockStore struct {
	mock.Mock
}

func (m *MockStore) GetUserByTelegramID(telegramID int64) (*storage.User, error) {
	args := m.Called(telegramID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.User), args.Error(1)
}

func (m *MockStore) GetUserByUsername(username string) (*storage.User, error) {
	args := m.Called(username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.User), args.Error(1)
}

func (m *MockStore) GetNotificationByID(id uint) (*storage.Notification, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.Notification), args.Error(1)
}

func (m *MockStore) GetNotificationByMessageID(messageID int) (*storage.Notification, error) {
	args := m.Called(messageID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.Notification), args.Error(1)
}

func (m *MockStore) CreateUser(user *storage.User) error {
	return m.Called(user).Error(0)
}

func (m *MockStore) CreateOrUpdateUser(user *storage.User) error {
	return m.Called(user).Error(0)
}

func (m *MockStore) UpdateUserChatID(telegramID, chatID int64) error {
	return m.Called(telegramID, chatID).Error(0)
}

func (m *MockStore) UpdateUserNames(telegramID int64, firstName, username string) error {
	return m.Called(telegramID, firstName, username).Error(0)
}

func (m *MockStore) UpdateNotificationMessageID(notificationID uint, messageID int) error {
	return m.Called(notificationID, messageID).Error(0)
}

func (m *MockStore) HasNotification(chatID int64, showTitle string, season, episodeNumber int) (bool, error) {
	args := m.Called(chatID, showTitle, season, episodeNumber)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) CreateNotification(notification *storage.Notification) error {
	return m.Called(notification).Error(0)
}

func (m *MockStore) HasUserInChat(chatID int64) (bool, error) {
	args := m.Called(chatID)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) GetTopics(chatID int64) ([]storage.Topic, error) {
	args := m.Called(chatID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.Topic), args.Error(1)
}

func (m *MockStore) CreateOrUpdateTopic(topic *storage.Topic) error {
	return m.Called(topic).Error(0)
}

func (m *MockStore) UpdateUserTokens(telegramID int64, accessToken, refreshToken string, expiresAt time.Time) error {
	return m.Called(telegramID, accessToken, refreshToken, expiresAt).Error(0)
}

func (m *MockStore) UpdateUserMuted(telegramID int64, muted bool) error {
	return m.Called(telegramID, muted).Error(0)
}

func (m *MockStore) GetDistinctChatIDs() ([]int64, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]int64), args.Error(1)
}

func (m *MockStore) GetUsersByChatID(chatID int64) ([]storage.User, error) {
	args := m.Called(chatID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.User), args.Error(1)
}

func (m *MockStore) CreateWatchStatuses(notificationID uint, userIDs []uint) error {
	return m.Called(notificationID, userIDs).Error(0)
}

func (m *MockStore) GetWatchStatuses(notificationID uint) ([]storage.WatchStatus, error) {
	args := m.Called(notificationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.WatchStatus), args.Error(1)
}

func (m *MockStore) GetUserWatchStatus(notificationID uint, userID uint) (storage.WatchStatus, error) {
	args := m.Called(notificationID, userID)
	return args.Get(0).(storage.WatchStatus), args.Error(1)
}

func (m *MockStore) GetUnwatchedStatusesByUser(userID uint) ([]storage.WatchStatus, error) {
	args := m.Called(userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.WatchStatus), args.Error(1)
}

func (m *MockStore) MarkWatchStatus(notificationID uint, userID uint) error {
	return m.Called(notificationID, userID).Error(0)
}

func (m *MockStore) UnmarkWatchStatus(notificationID uint, userID uint) error {
	return m.Called(notificationID, userID).Error(0)
}

func (m *MockStore) GetChatConfig(chatID int64) (*storage.ChatConfig, error) {
	args := m.Called(chatID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*storage.ChatConfig), args.Error(1)
}

func (m *MockStore) CreateOrUpdateChatConfig(config *storage.ChatConfig) error {
	return m.Called(config).Error(0)
}

func (m *MockStore) CreateScheduledDeletion(deletion *storage.ScheduledDeletion) error {
	return m.Called(deletion).Error(0)
}

func (m *MockStore) GetPendingDeletions() ([]storage.ScheduledDeletion, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]storage.ScheduledDeletion), args.Error(1)
}

func (m *MockStore) RemoveScheduledDeletion(id uint) error {
	return m.Called(id).Error(0)
}