package worker

import (
	"fmt"
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleUnsub(t *testing.T) {
	t.Run("sends auth prompt when user not found", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(nil, nil)

		w := newTestWorker(store, nil)

		w.handleUnsub(Task{
			ChatID:  42,
			Payload: UnsubPayload{TelegramID: 111, ChatID: 42},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "/sub first")
		store.AssertExpectations(t)
		store.AssertNotCalled(t, "UpdateUserMuted")
	})

	t.Run("mutes user and sends confirmation", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("UpdateUserMuted", mock.Anything, int64(111), true).Return(nil)

		w := newTestWorker(store, nil)

		w.handleUnsub(Task{
			ChatID:  42,
			Payload: UnsubPayload{TelegramID: 111, ChatID: 42},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Notifications paused")
		assert.Contains(t, result.Text, "@loraine")
		store.AssertCalled(t, "UpdateUserMuted", mock.Anything, int64(111), true)
		store.AssertExpectations(t)
	})

	t.Run("sends error message when DB update fails", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("UpdateUserMuted", mock.Anything, int64(111), true).Return(fmt.Errorf("db connection lost"))

		w := newTestWorker(store, nil)

		w.handleUnsub(Task{
			ChatID:  42,
			Payload: UnsubPayload{TelegramID: 111, ChatID: 42},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Failed to unsubscribe")
		store.AssertCalled(t, "UpdateUserMuted", mock.Anything, int64(111), true)
		store.AssertExpectations(t)
	})
}