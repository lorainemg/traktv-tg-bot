package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
)

func TestResolveConfig(t *testing.T) {
	t.Run("returns defaults when config is nil", func(t *testing.T) {
		country, timezone, deleteWatched, notifyHours := resolveConfig(nil)
		assert.Equal(t, "US", country)
		assert.Equal(t, "America/New_York", timezone)
		assert.True(t, deleteWatched)
		assert.Equal(t, 12, notifyHours)
	})

	t.Run("uses config values when set", func(t *testing.T) {
		config := &storage.ChatConfig{
			Country:       "GB",
			Timezone:      "Europe/London",
			DeleteWatched: false,
			NotifyHours:   24,
		}
		country, timezone, deleteWatched, notifyHours := resolveConfig(config)
		assert.Equal(t, "GB", country)
		assert.Equal(t, "Europe/London", timezone)
		assert.False(t, deleteWatched)
		assert.Equal(t, 24, notifyHours)
	})

	t.Run("falls back to defaults for empty string fields", func(t *testing.T) {
		config := &storage.ChatConfig{
			Country:     "",
			Timezone:    "",
			NotifyHours: 0,
		}
		country, timezone, _, notifyHours := resolveConfig(config)
		assert.Equal(t, "US", country)
		assert.Equal(t, "America/New_York", timezone)
		assert.Equal(t, 12, notifyHours)
	})
}

func TestHandleShowConfig(t *testing.T) {
	t.Run("sends config with defaults when no config exists", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetChatConfig", int64(42)).Return(nil, nil)

		w := newTestWorker(store, nil)

		w.handleShowConfig(Task{ChatID: 42})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Chat Settings")
		assert.Contains(t, result.Text, "US")
		assert.Contains(t, result.Text, "America/New_York")
		// Verify inline buttons are present for changing settings
		assert.NotNil(t, result.InlineButtons)
		store.AssertExpectations(t)
	})

	t.Run("sends config with stored values", func(t *testing.T) {
		store := &mocks.MockStore{}
		config := &storage.ChatConfig{
			Country:       "GB",
			Timezone:      "Europe/London",
			DeleteWatched: false,
			NotifyHours:   24,
		}
		store.On("GetChatConfig", int64(42)).Return(config, nil)

		w := newTestWorker(store, nil)

		w.handleShowConfig(Task{ChatID: 42})

		result := <-w.Results()
		assert.Contains(t, result.Text, "GB")
		assert.Contains(t, result.Text, "Europe/London")
		assert.Contains(t, result.Text, "Off")
		assert.Contains(t, result.Text, "24h")
		store.AssertExpectations(t)
	})
}