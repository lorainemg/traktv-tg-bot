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

func TestSortUpcomingEpisodes(t *testing.T) {
	t.Run("sorts by air date ascending", func(t *testing.T) {
		tomorrow := time.Now().Add(24 * time.Hour)
		nextWeek := time.Now().Add(7 * 24 * time.Hour)
		today := time.Now().Add(1 * time.Hour)

		episodeMap := map[string]*upcomingEpisode{
			"a": {Entry: trakt.CalendarEntry{FirstAired: tomorrow, Show: trakt.Show{Title: "Show B"}}},
			"b": {Entry: trakt.CalendarEntry{FirstAired: nextWeek, Show: trakt.Show{Title: "Show C"}}},
			"c": {Entry: trakt.CalendarEntry{FirstAired: today, Show: trakt.Show{Title: "Show A"}}},
		}

		result := sortUpcomingEpisodes(episodeMap)

		assert.Len(t, result, 3)
		assert.Equal(t, "Show A", result[0].Entry.Show.Title) // soonest
		assert.Equal(t, "Show B", result[1].Entry.Show.Title)
		assert.Equal(t, "Show C", result[2].Entry.Show.Title) // furthest
	})

	t.Run("returns empty for empty map", func(t *testing.T) {
		result := sortUpcomingEpisodes(map[string]*upcomingEpisode{})
		assert.Empty(t, result)
	})
}

func TestHandleUpcoming(t *testing.T) {
	t.Run("sends no users message when chat is empty", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUsersByChatID", int64(42)).Return([]storage.User{}, nil)

		w := newTestWorker(store, nil)

		w.handleUpcoming(Task{
			ChatID:  42,
			Payload: 7,
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "No subscribed users")
		store.AssertExpectations(t)
	})

	t.Run("sends no episodes message when calendar is empty", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		users := []storage.User{
			{TelegramID: 111, TraktAccessToken: "t1", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
		}
		store.On("GetUsersByChatID", int64(42)).Return(users, nil)
		store.On("GetChatConfig", int64(42)).Return(nil, nil)

		// Calendar returns empty
		traktMock.On("GetCalendar", mock.Anything, mock.Anything, 7).Return([]trakt.CalendarEntry{}, nil)
		traktMock.On("GetWatchlistShows", mock.Anything).Return(map[int]bool(nil), nil)

		w := newTestWorker(store, traktMock)

		w.handleUpcoming(Task{
			ChatID:  42,
			Payload: 7,
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "No upcoming episodes")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("defaults to 7 days when payload is invalid", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		users := []storage.User{
			{TelegramID: 111, TraktAccessToken: "t1", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
		}
		store.On("GetUsersByChatID", int64(42)).Return(users, nil)
		store.On("GetChatConfig", int64(42)).Return(nil, nil)

		// The key assertion: GetCalendar is called with days=7 (the default)
		traktMock.On("GetCalendar", mock.Anything, mock.Anything, 7).Return([]trakt.CalendarEntry{}, nil)
		traktMock.On("GetWatchlistShows", mock.Anything).Return(map[int]bool(nil), nil)

		w := newTestWorker(store, traktMock)

		w.handleUpcoming(Task{
			ChatID:  42,
			Payload: "not an int", // invalid payload → falls back to 7
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "No upcoming episodes in the next 7 days")
		traktMock.AssertExpectations(t)
	})

	t.Run("sends upcoming episodes with show info", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		users := []storage.User{
			{TelegramID: 111, Username: "loraine", TraktAccessToken: "t1", TraktTokenExpiresAt: time.Now().Add(48 * time.Hour)},
		}
		store.On("GetUsersByChatID", int64(42)).Return(users, nil)
		store.On("GetChatConfig", int64(42)).Return(nil, nil)

		traktMock.On("GetWatchlistShows", mock.Anything).Return(map[int]bool(nil), nil)
		traktMock.On("GetCalendar", mock.Anything, mock.Anything, 7).Return([]trakt.CalendarEntry{
			{
				FirstAired: time.Now().Add(2 * time.Hour),
				Show:       trakt.Show{Title: "Severance", IDs: trakt.ShowIDs{Trakt: 100, Slug: "severance"}},
				Episode:    trakt.Episode{Season: 2, Number: 5, Title: "Goodbye"},
			},
		}, nil)

		w := newTestWorker(store, traktMock)

		w.handleUpcoming(Task{
			ChatID:  42,
			Payload: 7,
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Upcoming episodes")
		assert.Contains(t, result.Text, "Severance")
		assert.Contains(t, result.Text, "S02E05")
		assert.Contains(t, result.Text, "@loraine")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}