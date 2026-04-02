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

func TestCollectUnseenShows(t *testing.T) {
	entries := []trakt.WatchedShowEntry{
		{Plays: 5, Show: trakt.Show{Title: "Severance", AiredEpisodes: 10}},   // 5 unseen
		{Plays: 20, Show: trakt.Show{Title: "Breaking Bad", AiredEpisodes: 20}}, // caught up
		{Plays: 8, Show: trakt.Show{Title: "The Bear", AiredEpisodes: 10}},     // 2 unseen
		{Plays: 1, Show: trakt.Show{Title: "Shogun", AiredEpisodes: 10}},       // 9 unseen
	}

	t.Run("filters out caught-up shows", func(t *testing.T) {
		result := collectUnseenShows(entries)
		assert.Len(t, result, 3)
		for _, show := range result {
			assert.NotEqual(t, "Breaking Bad", show.Show.Title)
		}
	})

	t.Run("calculates correct unseen count", func(t *testing.T) {
		result := collectUnseenShows(entries)
		for _, show := range result {
			if show.Show.Title == "Severance" {
				assert.Equal(t, 5, show.Unseen)
				return
			}
		}
		t.Error("Severance not found in results")
	})

	t.Run("sorts by unseen count descending", func(t *testing.T) {
		result := collectUnseenShows(entries)
		assert.Equal(t, "Shogun", result[0].Show.Title)
		assert.Equal(t, "Severance", result[1].Show.Title)
		assert.Equal(t, "The Bear", result[2].Show.Title)
	})

	t.Run("returns empty for all caught-up", func(t *testing.T) {
		caughtUp := []trakt.WatchedShowEntry{
			{Plays: 10, Show: trakt.Show{AiredEpisodes: 10}},
		}
		assert.Empty(t, collectUnseenShows(caughtUp))
	})
}

func TestHandleUnseen(t *testing.T) {
	t.Run("sends auth prompt when user not found", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(nil, nil)

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
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)

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
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		traktMock.On("GetWatchedShows", mock.Anything, mock.AnythingOfType("trakt.TokenSource")).Return(
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
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		traktMock.On("GetWatchedShows", mock.Anything, mock.AnythingOfType("trakt.TokenSource")).Return(
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