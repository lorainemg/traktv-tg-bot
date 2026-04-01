package worker

import (
	"fmt"
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestFilterReturningShows(t *testing.T) {
	entries := []trakt.WatchedShowEntry{
		{Show: trakt.Show{Title: "Breaking Bad", Status: trakt.ShowStatusEnded}},
		{Show: trakt.Show{Title: "Severance", Status: trakt.ShowStatusReturning}},
		{Show: trakt.Show{Title: "The Bear", Status: trakt.ShowStatusReturning}},
		{Show: trakt.Show{Title: "Mindhunter", Status: trakt.ShowStatusCanceled}},
	}

	t.Run("keeps only returning shows", func(t *testing.T) {
		result := filterReturningShows(entries)
		assert.Len(t, result, 2)
		for _, show := range result {
			assert.Equal(t, trakt.ShowStatusReturning, show.Status)
		}
	})

	t.Run("sorts alphabetically by title", func(t *testing.T) {
		result := filterReturningShows(entries)
		assert.Equal(t, "Severance", result[0].Title)
		assert.Equal(t, "The Bear", result[1].Title)
	})

	t.Run("returns empty slice when no shows are returning", func(t *testing.T) {
		ended := []trakt.WatchedShowEntry{
			{Show: trakt.Show{Title: "Breaking Bad", Status: trakt.ShowStatusEnded}},
		}
		result := filterReturningShows(ended)
		assert.Empty(t, result)
	})
}

func TestFormatShowsMessage(t *testing.T) {
	user := &storage.User{FirstName: "Loraine"}
	shows := []trakt.Show{
		{Title: "Severance", IDs: trakt.ShowIDs{Slug: "severance"}},
		{Title: "The Bear", IDs: trakt.ShowIDs{Slug: "the-bear"}},
	}

	t.Run("single page has no range indicator", func(t *testing.T) {
		result := formatShowsMessage(shows, 0, 1, 2, user)

		assert.Contains(t, result, "Loraine's followed shows")
		assert.NotContains(t, result, "of")
		assert.Contains(t, result, "[Severance](https://trakt.tv/shows/severance)")
		assert.Contains(t, result, "[The Bear](https://trakt.tv/shows/the-bear)")
	})

	t.Run("multi-page shows range indicator", func(t *testing.T) {
		result := formatShowsMessage(shows, 0, 3, 45, user)

		assert.Contains(t, result, "Loraine's followed shows")
		assert.Contains(t, result, "1–2 of 45")
		assert.Contains(t, result, "[The Bear](https://trakt.tv/shows/the-bear)")
	})
}

func TestHandleShows(t *testing.T) {
	t.Run("sends auth prompt when user not found", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUserByTelegramID", int64(111)).Return(nil, nil)

		w := newTestWorker(store, nil)

		w.handleShows(Task{
			ChatID:  42,
			Payload: UserTarget{RequesterID: 111},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "/sub")
		store.AssertExpectations(t)
	})

	t.Run("sends no shows message when all are ended/canceled", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		user := &storage.User{
			TelegramID:          111,
			Username:            "loraine",
			FirstName:           "Loraine",
			TraktAccessToken:    "token123",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)
		traktMock.On("GetWatchedShows", mock.AnythingOfType("trakt.TokenSource")).Return(
			[]trakt.WatchedShowEntry{
				{Show: trakt.Show{Title: "Breaking Bad", Status: trakt.ShowStatusEnded}},
			}, nil,
		)

		w := newTestWorker(store, traktMock)

		w.handleShows(Task{
			ChatID:  42,
			Payload: UserTarget{RequesterID: 111},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "isn't watching any returning shows")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("sends shows list with no pagination for small lists", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		user := &storage.User{
			TelegramID:          111,
			FirstName:           "Loraine",
			TraktAccessToken:    "token123",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)
		traktMock.On("GetWatchedShows", mock.AnythingOfType("trakt.TokenSource")).Return(
			[]trakt.WatchedShowEntry{
				{Show: trakt.Show{Title: "Severance", Status: trakt.ShowStatusReturning, IDs: trakt.ShowIDs{Slug: "severance"}}},
				{Show: trakt.Show{Title: "The Bear", Status: trakt.ShowStatusReturning, IDs: trakt.ShowIDs{Slug: "the-bear"}}},
			}, nil,
		)

		w := newTestWorker(store, traktMock)

		w.handleShows(Task{
			ChatID:  42,
			Payload: UserTarget{RequesterID: 111},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Loraine's followed shows")
		assert.Contains(t, result.Text, "Severance")
		assert.Contains(t, result.Text, "The Bear")
		assert.Nil(t, result.InlineButtons)
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("sends shows list with pagination for large lists", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		user := &storage.User{
			TelegramID:          111,
			FirstName:           "Loraine",
			TraktAccessToken:    "token123",
			TraktTokenExpiresAt: time.Now().Add(48 * time.Hour),
		}
		store.On("GetUserByTelegramID", int64(111)).Return(user, nil)
		entries := make([]trakt.WatchedShowEntry, 25)
		for i := range entries {
			entries[i] = trakt.WatchedShowEntry{
				Show: trakt.Show{
					Title:  fmt.Sprintf("Show %d", i),
					Status: trakt.ShowStatusReturning,
				},
			}
		}
		traktMock.On("GetWatchedShows", mock.AnythingOfType("trakt.TokenSource")).Return(entries, nil)

		w := newTestWorker(store, traktMock)

		w.handleShows(Task{
			ChatID:  42,
			Payload: UserTarget{RequesterID: 111},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Loraine's followed shows")
		assert.NotNil(t, result.InlineButtons)
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}