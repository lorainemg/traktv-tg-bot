package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
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