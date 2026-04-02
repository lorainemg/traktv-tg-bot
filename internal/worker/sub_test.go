package worker

import (
	"fmt"
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleSub(t *testing.T) {
	t.Run("sends already subscribed when user exists in same chat", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, ChatID: 42, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("UpdateUserNames", mock.Anything, int64(111), "Loraine", "loraine").Return(nil)

		w := newTestWorker(store, nil)

		w.handleSub(Task{
			ChatID: 42,
			Payload: SubPayload{
				TelegramID: 111,
				ChatID:     42,
				FirstName:  "Loraine",
				Username:   "loraine",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "already subscribed")
		store.AssertExpectations(t)
	})

	t.Run("re-subscribes muted user", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, ChatID: 42, Username: "loraine", Muted: true}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("UpdateUserMuted", mock.Anything, int64(111), false).Return(nil)
		store.On("UpdateUserNames", mock.Anything, int64(111), "Loraine", "loraine").Return(nil)

		w := newTestWorker(store, nil)

		w.handleSub(Task{
			ChatID: 42,
			Payload: SubPayload{
				TelegramID: 111,
				ChatID:     42,
				FirstName:  "Loraine",
				Username:   "loraine",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Welcome back")
		store.AssertCalled(t, "UpdateUserMuted", mock.Anything, int64(111), false)
		store.AssertExpectations(t)
	})

	t.Run("starts device code flow for new user", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(nil, nil)
		traktMock.On("RequestDeviceCode", mock.Anything).Return(&trakt.DeviceCode{
			DeviceCode:      "abc123",
			UserCode:        "XYZQ",
			VerificationURL: "https://trakt.tv/activate",
			Interval:        1, // 1 second so the goroutine doesn't block long
			ExpiresIn:       1, // expires immediately so the goroutine exits fast
		}, nil)
		// PollForToken is called in a background goroutine. Return an error
		// so it exits cleanly instead of looping. .Maybe() marks this as
		// optional — the goroutine may exit via timeout before calling it.
		traktMock.On("PollForToken", mock.Anything, "abc123").Return(nil, fmt.Errorf("auth expired")).Maybe()

		// Buffer size 3: one for the device code message, one for the
		// goroutine's delete of the code message, one for the error/timeout message.
		w := New(store, traktMock, nil, 3)

		w.handleSub(Task{
			ChatID: 42,
			Payload: SubPayload{
				TelegramID: 111,
				ChatID:     42,
				FirstName:  "Loraine",
				Username:   "loraine",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "https://trakt.tv/activate")
		assert.Contains(t, result.Text, "XYZQ")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("sends error when device code request fails", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(nil, nil)
		traktMock.On("RequestDeviceCode", mock.Anything).Return(nil, fmt.Errorf("trakt API down"))

		w := newTestWorker(store, traktMock)

		w.handleSub(Task{
			ChatID: 42,
			Payload: SubPayload{
				TelegramID: 111,
				ChatID:     42,
				FirstName:  "Loraine",
				Username:   "loraine",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Failed to start Trakt auth")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("sends DB error message when lookup fails", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(nil, fmt.Errorf("connection refused"))

		w := newTestWorker(store, nil)

		w.handleSub(Task{
			ChatID: 42,
			Payload: SubPayload{
				TelegramID: 111,
				ChatID:     42,
				FirstName:  "Loraine",
				Username:   "loraine",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Something went wrong")
		store.AssertExpectations(t)
	})
}

func TestHandleExistingUserSub(t *testing.T) {
	t.Run("moves notifications to new chat", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, ChatID: 99, Username: "loraine"} // old chat 99
		store.On("UpdateUserChatID", mock.Anything, int64(111), int64(42)).Return(nil)

		// Buffer size 2: farewell to old chat + confirmation in new chat
		w := New(store, nil, nil, 2)

		w.handleExistingUserSub(
			Task{ChatID: 42},
			SubPayload{TelegramID: 111, ChatID: 42},
			user,
		)

		// First result: farewell to old chat
		farewell := <-w.Results()
		assert.Equal(t, int64(99), farewell.ChatID)
		assert.Contains(t, farewell.Text, "moved their notifications")

		// Second result: confirmation in new chat
		confirmation := <-w.Results()
		assert.Contains(t, confirmation.Text, "Notifications will now be sent here")
		store.AssertExpectations(t)
	})
}