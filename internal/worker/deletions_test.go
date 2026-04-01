package worker

import (
	"fmt"
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestHandleProcessDeletions(t *testing.T) {
	t.Run("sends delete results and cleans up DB records", func(t *testing.T) {
		store := &mocks.MockStore{}
		deletions := []storage.ScheduledDeletion{
			{Model: gorm.Model{ID: 1}, ChatID: 42, MessageID: 100},
			{Model: gorm.Model{ID: 2}, ChatID: 42, MessageID: 200},
		}
		store.On("GetPendingDeletions").Return(deletions, nil)
		store.On("RemoveScheduledDeletion", uint(1)).Return(nil)
		store.On("RemoveScheduledDeletion", uint(2)).Return(nil)

		// Buffer 2 — one per deletion
		w := New(store, nil, nil, 2)

		w.handleProcessDeletions()

		r1 := <-w.Results()
		assert.Equal(t, int64(42), r1.ChatID)
		assert.Equal(t, 100, r1.DeleteMessageID)

		r2 := <-w.Results()
		assert.Equal(t, 200, r2.DeleteMessageID)

		store.AssertExpectations(t)
	})

	t.Run("continues deleting when RemoveScheduledDeletion fails", func(t *testing.T) {
		store := &mocks.MockStore{}
		deletions := []storage.ScheduledDeletion{
			{Model: gorm.Model{ID: 1}, ChatID: 42, MessageID: 100},
			{Model: gorm.Model{ID: 2}, ChatID: 42, MessageID: 200},
		}
		store.On("GetPendingDeletions").Return(deletions, nil)
		// First cleanup fails — handler should still process the second deletion
		store.On("RemoveScheduledDeletion", uint(1)).Return(fmt.Errorf("db error"))
		store.On("RemoveScheduledDeletion", uint(2)).Return(nil)

		w := New(store, nil, nil, 2)

		w.handleProcessDeletions()

		r1 := <-w.Results()
		r2 := <-w.Results()
		// Both messages should still be sent for deletion even if DB cleanup fails
		assert.Equal(t, 100, r1.DeleteMessageID)
		assert.Equal(t, 200, r2.DeleteMessageID)
		store.AssertExpectations(t)
	})

	t.Run("does nothing when no pending deletions", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetPendingDeletions").Return([]storage.ScheduledDeletion{}, nil)

		w := newTestWorker(store, nil)

		w.handleProcessDeletions()

		// No results should be sent — channel should be empty
		assert.Len(t, w.Results(), 0)
		store.AssertExpectations(t)
	})
}