package worker

import (
	"strings"
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestFormatWhoWatchesMessage(t *testing.T) {
	showLink := "[Breaking Bad](https://trakt.tv/shows/breaking-bad)"

	t.Run("lists watchers", func(t *testing.T) {
		watchers := []*storage.User{
			{Username: "loraine"},
			{FirstName: "Bob", TelegramID: 222},
		}

		result := formatWhoWatchesMessage(showLink, watchers)

		assert.Contains(t, result, "Who watches")
		assert.Contains(t, result, "Breaking Bad")
		assert.Contains(t, result, "[@loraine](https://t.me/loraine)")
		assert.Contains(t, result, "[Bob](tg://user?id=222)")
	})

	t.Run("lists watchers when no users are provided", func(t *testing.T) {
		var watchers []*storage.User

		result := formatWhoWatchesMessage(showLink, watchers)

		assert.Contains(t, result, "Nobody in this chat watches this show.")
	})
}

func TestHandleWhoWatches(t *testing.T) {
	t.Run("sends not found when search returns no results", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		traktMock.On("SearchShows", mock.Anything, "nonexistent show").Return([]trakt.SearchResult{}, nil)

		w := newTestWorker(store, traktMock)

		w.handleWhoWatches(Task{
			ChatID:  42,
			Payload: WhoWatchesPayload{Query: "nonexistent show"},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "No shows found")
		traktMock.AssertExpectations(t)
	})

	t.Run("sends watchers list when users watch the show", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		traktMock.On("SearchShows", mock.Anything, "breaking bad").Return([]trakt.SearchResult{
			{Show: trakt.Show{Title: "Breaking Bad", IDs: trakt.ShowIDs{Trakt: 1388, Slug: "breaking-bad"}}},
		}, nil)

		users := []storage.User{
			{TelegramID: 111, Username: "loraine", TraktAccessToken: "t1", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
			{TelegramID: 222, FirstName: "Bob", TraktAccessToken: "t2", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
		}
		store.On("GetUsersByChatID", mock.Anything, int64(42)).Return(users, nil)

		traktMock.On("GetWatchedShows", mock.Anything, mock.Anything).Return(
			[]trakt.WatchedShowEntry{
				{Show: trakt.Show{IDs: trakt.ShowIDs{Trakt: 1388}}},
			}, nil,
		)

		w := newTestWorker(store, traktMock)

		w.handleWhoWatches(Task{
			ChatID:  42,
			Payload: WhoWatchesPayload{Query: "breaking bad"},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Who watches")
		assert.Contains(t, result.Text, "Breaking Bad")
		assert.Contains(t, result.Text, "@loraine")
		assert.Contains(t, result.Text, "Bob")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("only lists users who actually watch the show", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		traktMock.On("SearchShows", mock.Anything, "breaking bad").Return([]trakt.SearchResult{
			{Show: trakt.Show{Title: "Breaking Bad", IDs: trakt.ShowIDs{Trakt: 1388, Slug: "breaking-bad"}}},
		}, nil)

		users := []storage.User{
			{TelegramID: 111, Username: "loraine", TraktAccessToken: "t1", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
			{TelegramID: 222, FirstName: "Bob", TraktAccessToken: "t2", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
		}
		store.On("GetUsersByChatID", mock.Anything, int64(42)).Return(users, nil)

		traktMock.On("GetWatchedShows", mock.Anything, mock.Anything).Return(
			[]trakt.WatchedShowEntry{
				{Show: trakt.Show{IDs: trakt.ShowIDs{Trakt: 1388}}},
			}, nil,
		).Once()
		traktMock.On("GetWatchedShows", mock.Anything, mock.Anything).Return(
			[]trakt.WatchedShowEntry{
				{Show: trakt.Show{IDs: trakt.ShowIDs{Trakt: 9999}}},
			}, nil,
		).Once()

		w := newTestWorker(store, traktMock)

		w.handleWhoWatches(Task{
			ChatID:  42,
			Payload: WhoWatchesPayload{Query: "breaking bad"},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Who watches")
		assert.Equal(t, 1, strings.Count(result.Text, "▸"))
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}
