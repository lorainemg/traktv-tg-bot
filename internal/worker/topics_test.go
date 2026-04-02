package worker

import (
	"fmt"
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandleRegisterTopic(t *testing.T) {
	t.Run("registers topic and sends confirmation", func(t *testing.T) {
		store := &mocks.MockStore{}
		// mock.MatchedBy lets us assert on the argument passed to CreateOrUpdateTopic
		// without matching the exact pointer. We check the fields instead.
		store.On("CreateOrUpdateTopic", mock.Anything, mock.MatchedBy(func(topic *storage.Topic) bool {
			return topic.ChatID == 42 && topic.ThreadID == 10 && topic.Name == "anime"
		})).Return(nil)

		w := newTestWorker(store, nil)

		w.handleRegisterTopic(Task{
			ChatID: 42,
			Payload: TopicPayload{
				ChatID:   42,
				ThreadID: 10,
				Name:     "Anime", // should be lowercased
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Topic registered")
		assert.Contains(t, result.Text, "Anime")
		store.AssertExpectations(t)
	})

	t.Run("sends error when DB write fails", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("CreateOrUpdateTopic", mock.Anything, mock.Anything).Return(fmt.Errorf("db error"))

		w := newTestWorker(store, nil)

		w.handleRegisterTopic(Task{
			ChatID: 42,
			Payload: TopicPayload{
				ChatID:   42,
				ThreadID: 10,
				Name:     "anime",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Failed to register topic")
		store.AssertExpectations(t)
	})
}
