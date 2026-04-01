package worker

import (
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// newTestWorker creates a Worker with mock store and trakt for testing.
// bufferSize=1 on the results channel means we can read one result
// without blocking — enough for handler tests that send a single response.
func newTestWorker(store *mocks.MockStore, traktMock *mocks.MockTrakt) *Worker {
	return New(store, traktMock, nil, 1)
}

func TestResolveTargetUser(t *testing.T) {
	t.Run("looks up by username when TargetUsername is set", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{Username: "loraine", TelegramID: 222}
		store.On("GetUserByUsername", "loraine").Return(user, nil)

		w := newTestWorker(store, nil)

		result, err := w.resolveTargetUser(UserTarget{
			RequesterID:    111,
			TargetUsername: "loraine",
		})

		assert.NoError(t, err)
		assert.Equal(t, "loraine", result.Username)
		assert.Equal(t, int64(222), result.TelegramID)
		store.AssertExpectations(t)
	})

	t.Run("falls back to RequesterID when no target is specified", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, FirstName: "Me"}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)

		w := newTestWorker(store, nil)

		result, err := w.resolveTargetUser(UserTarget{
			RequesterID: 111,
		})

		assert.NoError(t, err)
		assert.Equal(t, int64(111), result.TelegramID)
		store.AssertExpectations(t)
	})

	t.Run("looks up by TargetTelegramID when replying to a message", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{Username: "loraine", TelegramID: 222}
		store.On("GetUserByTelegramID", int64(222)).Return(user, nil)

		w := newTestWorker(store, nil)

		result, err := w.resolveTargetUser(UserTarget{
			RequesterID:      111,
			TargetTelegramID: 222,
		})

		assert.NoError(t, err)
		assert.Equal(t, "loraine", result.Username)
		assert.Equal(t, int64(222), result.TelegramID)
		store.AssertExpectations(t)
	})
}

func TestHandleUnseen(t *testing.T) {
	t.Run("sends auth prompt when user not found", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUserByTelegramID", int64(111)).Return(nil, nil)

		w := newTestWorker(store, nil)

		w.handleUnseen(Task{
			ChatID: 42,
			Payload: UnseenPayload{
				UserTarget: UserTarget{RequesterID: 111},
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "/sub")
		store.AssertExpectations(t)
	})

	t.Run("sends muted message when user is unsubscribed", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, Username: "loraine", Muted: true}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)

		w := newTestWorker(store, nil)

		w.handleUnseen(Task{
			ChatID: 42,
			Payload: UnseenPayload{
				UserTarget: UserTarget{RequesterID: 111},
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "unsubscribed")
		store.AssertExpectations(t)
	})

	t.Run("sends all caught up when no unseen episodes", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		user := &storage.User{
			TelegramID:          111,
			Username:            "loraine",
			TraktAccessToken:    "token123",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)
		traktMock.On("GetWatchedShows", mock.AnythingOfType("trakt.TokenSource")).Return(
			[]trakt.WatchedShowEntry{
				{Plays: 10, Show: trakt.Show{Title: "Severance", AiredEpisodes: 10}},
			}, nil,
		)

		w := newTestWorker(store, traktMock)

		w.handleUnseen(Task{
			ChatID: 42,
			Payload: UnseenPayload{
				UserTarget: UserTarget{RequesterID: 111},
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "caught up")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("user has unseen episodes", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)
		traktMock.On("GetWatchedShows", mock.AnythingOfType("trakt.TokenSource")).Return(
			[]trakt.WatchedShowEntry{
				{Plays: 8, Show: trakt.Show{Title: "Severance", IDs: trakt.ShowIDs{Slug: "severance"}, AiredEpisodes: 10}},
			}, nil,
		)

		w := newTestWorker(store, traktMock)

		w.handleUnseen(Task{
			ChatID: 42,
			Payload: UnseenPayload{
				UserTarget: UserTarget{RequesterID: 111},
			},
		})

		result := <-w.Results()

		assert.Contains(t, result.Text, "Unseen episodes for")
		assert.Contains(t, result.Text, "Severance")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}
