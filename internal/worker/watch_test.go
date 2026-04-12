package worker

import (
	"fmt"
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
)

// watchTestPayload builds a WatchActionPayload with common defaults.
func watchTestPayload() WatchActionPayload {
	return WatchActionPayload{
		TelegramID:       111,
		ChatID:           42,
		NotificationID:   1,
		NotificationType: storage.NotificationEpisode,
		CallbackQueryID:  "cbq-123",
	}
}

// airedNotification returns a notification with a FirstAired time in the past.
func airedNotification() *storage.Notification {
	return &storage.Notification{
		Model:         gorm.Model{ID: 1},
		TraktShowID:   100,
		ShowTitle:     "Severance",
		Season:        2,
		EpisodeNumber: 5,
		FirstAired:    time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
}

func TestHandleMarkWatched(t *testing.T) {
	t.Run("rejects if episode hasn't aired yet", func(t *testing.T) {
		store := &mocks.MockStore{}
		futureNotification := &storage.Notification{
			Model:      gorm.Model{ID: 1},
			FirstAired: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}
		// User is resolved first in the new flow, then the notification is looked up
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(futureNotification, nil)

		w := newTestWorker(store, nil)

		w.handleMarkWatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "hasn't aired yet")
		assert.True(t, result.CallbackShowAlert)
		store.AssertExpectations(t)
	})

	t.Run("rejects if user is not following the show", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(airedNotification(), nil)
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		// Zero ID means no watch status found — user isn't following
		store.On("GetUserWatchStatus", mock.Anything, uint(1), uint(0)).Return(storage.WatchStatus{}, nil)

		w := newTestWorker(store, nil)

		w.handleMarkWatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "not following")
		store.AssertExpectations(t)
	})

	t.Run("rejects if already watched", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(airedNotification(), nil)
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		// Already watched — expectWatched is false for MarkWatched, so mismatch
		store.On("GetUserWatchStatus", mock.Anything, uint(1), uint(0)).Return(storage.WatchStatus{
			Model:   gorm.Model{ID: 5},
			Watched: true,
		}, nil)

		w := newTestWorker(store, nil)

		w.handleMarkWatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "already watched")
		store.AssertExpectations(t)
	})

	t.Run("marks episode as watched on Trakt and DB", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		notification := airedNotification()
		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(notification, nil)

		user := &storage.User{
			TelegramID:          111,
			Username:            "loraine",
			TraktAccessToken:    "token",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("GetUserWatchStatus", mock.Anything, uint(1), uint(0)).Return(storage.WatchStatus{
			Model:   gorm.Model{ID: 5},
			Watched: false, // not yet watched — matches expectWatched=false
		}, nil)

		// Trakt API call to mark watched
		traktMock.On("MarkEpisodeWatched", mock.Anything, mock.Anything, 100, 2, 5).Return(nil)
		// DB update
		store.On("MarkWatchStatus", mock.Anything, uint(1), uint(0)).Return(nil)

		// refreshNotificationMessage calls: GetChatConfig, GetWatchStatuses
		// nil config → defaults (deleteWatched=true), so when allWatched=true
		// it also calls CreateScheduledDeletion
		store.On("GetChatConfig", mock.Anything, int64(42)).Return(nil, nil)
		store.On("GetWatchStatusesByType", mock.Anything, storage.NotificationEpisode, uint(1)).Return([]storage.WatchStatus{
			{Watched: true, User: storage.User{Username: "loraine"}},
		}, nil)
		store.On("CreateScheduledDeletion", mock.Anything, mock.Anything).Return(nil)

		// Buffer 2: callback answer + edited notification message
		w := New(store, traktMock, nil, 2)

		w.handleMarkWatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		// First result: callback toast
		toast := <-w.Results()
		assert.Equal(t, "Marked as watched!", toast.Text)
		assert.False(t, toast.CallbackShowAlert) // success = no alert

		// Second result: refreshed notification message
		edit := <-w.Results()
		assert.Contains(t, edit.Text, "Severance")
		// All users watched → "All caught up" and no watch buttons (keyboard removed)
		assert.Contains(t, edit.Text, "All caught up")
		assert.Nil(t, edit.InlineButtons)

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("sends failure toast when Trakt API fails", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(airedNotification(), nil)
		user := &storage.User{
			TelegramID:          111,
			TraktAccessToken:    "token",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("GetUserWatchStatus", mock.Anything, uint(1), uint(0)).Return(storage.WatchStatus{
			Model:   gorm.Model{ID: 5},
			Watched: false,
		}, nil)
		traktMock.On("MarkEpisodeWatched", mock.Anything, mock.Anything, 100, 2, 5).Return(fmt.Errorf("trakt 500"))

		w := newTestWorker(store, traktMock)

		w.handleMarkWatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Failed to mark as watched")
		assert.True(t, result.CallbackShowAlert) // error = show alert
		// MarkWatchStatus should NOT have been called — Trakt failed first
		store.AssertNotCalled(t, "MarkWatchStatus")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}

func TestHandleMarkUnwatched(t *testing.T) {
	t.Run("unmarks a watched episode", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		notification := airedNotification()
		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(notification, nil)

		user := &storage.User{
			TelegramID:          111,
			Username:            "loraine",
			TraktAccessToken:    "token",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("GetUserWatchStatus", mock.Anything, uint(1), uint(0)).Return(storage.WatchStatus{
			Model:   gorm.Model{ID: 5},
			Watched: true, // currently watched — matches expectWatched=true
		}, nil)

		traktMock.On("UnmarkEpisodeWatched", mock.Anything, mock.Anything, 100, 2, 5).Return(nil)
		store.On("UnmarkWatchStatus", mock.Anything, uint(1), uint(0)).Return(nil)

		// refreshNotificationMessage
		store.On("GetChatConfig", mock.Anything, int64(42)).Return(nil, nil)
		store.On("GetWatchStatusesByType", mock.Anything, storage.NotificationEpisode, uint(1)).Return([]storage.WatchStatus{
			{Watched: false, User: storage.User{Username: "loraine"}},
		}, nil)

		w := New(store, traktMock, nil, 2)

		w.handleMarkUnwatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		toast := <-w.Results()
		assert.Equal(t, "Unmarked as watched!", toast.Text)

		edit := <-w.Results()
		assert.Contains(t, edit.Text, "Severance")
		// Watch buttons should still be present (not all watched)
		assert.NotNil(t, edit.InlineButtons)

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("rejects if not yet watched", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetNotificationByID", mock.Anything, uint(1)).Return(airedNotification(), nil)
		user := &storage.User{TelegramID: 111}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("GetUserWatchStatus", mock.Anything, uint(1), uint(0)).Return(storage.WatchStatus{
			Model:   gorm.Model{ID: 5},
			Watched: false, // not watched — can't unwatch
		}, nil)

		w := newTestWorker(store, nil)

		w.handleMarkUnwatched(Task{
			ChatID:  42,
			Payload: watchTestPayload(),
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "haven't watched")
		store.AssertExpectations(t)
	})
}