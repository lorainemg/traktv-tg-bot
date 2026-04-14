package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/mocks"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// testTrendingMovie builds a TrendingMovie with common defaults for testing.
func testTrendingMovie(traktID int, title string) trakt.TrendingMovie {
	return trakt.TrendingMovie{
		Watchers: 10,
		Movie: trakt.Movie{
			Title:    title,
			Year:     2025,
			IDs:      trakt.MovieIDs{Trakt: traktID, Slug: title, IMDB: "tt" + fmt.Sprint(traktID)},
			Runtime:  120,
			Rating:   7.5,
			Genres:   []string{"drama"},
			Overview: "A test movie overview.",
		},
	}
}

func TestHandleSubscribeMovies(t *testing.T) {
	t.Run("subscribes when no existing subscription", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("GetMovieSubscription", mock.Anything, uint(0), "all").Return(nil, nil)
		store.On("CreateMovieSubscription", mock.Anything, mock.AnythingOfType("*storage.MovieSubscription")).Return(nil)

		// After subscribing, sendInitialTrending fetches movies immediately.
		// An empty list means the user gets a "no new movies" message.
		traktMock.On("GetTrendingMovies", mock.Anything, 50).Return([]trakt.TrendingMovie{}, nil)

		// Buffer 2: subscription confirmation + "no new movies" message
		w := New(store, traktMock, nil, 2)

		w.handleSubscribeMovies(Task{
			ChatID: 42,
			Payload: MovieSubscriptionPayload{
				TelegramID: 111,
				ChatID:     42,
				Type:       "all",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Subscribed")

		// Verify initial trending fetch fired
		noMovies := <-w.Results()
		assert.Contains(t, noMovies.Text, "No new trending movies")

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("unsubscribes when already subscribed", func(t *testing.T) {
		store := &mocks.MockStore{}
		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		existing := &storage.MovieSubscription{UserID: user.ID, ChatID: 42, Type: "all"}
		store.On("GetMovieSubscription", mock.Anything, uint(0), "all").Return(existing, nil)
		store.On("DeleteMovieSubscription", mock.Anything, uint(0), "all").Return(nil)

		w := newTestWorker(store, nil)

		w.handleSubscribeMovies(Task{
			ChatID: 42,
			Payload: MovieSubscriptionPayload{
				TelegramID: 111,
				ChatID:     42,
				Type:       "all",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "Unsubscribed")
		store.AssertExpectations(t)
	})

	t.Run("rejects if user not linked", func(t *testing.T) {
		store := &mocks.MockStore{}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(nil, nil)

		w := newTestWorker(store, nil)

		w.handleSubscribeMovies(Task{
			ChatID: 42,
			Payload: MovieSubscriptionPayload{
				TelegramID: 111,
				ChatID:     42,
				Type:       "all",
			},
		})

		result := <-w.Results()
		assert.Contains(t, result.Text, "link your Trakt account")
		store.AssertExpectations(t)
	})
}

func TestHandleFollowMovie(t *testing.T) {
	t.Run("posts movie to group chat and advances session", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)

		// Save as followed
		store.On("CreateFollowedMovie", mock.Anything, mock.AnythingOfType("*storage.FollowedMovie")).Return(nil)

		// postMovieToGroupChat: not already posted
		store.On("HasMovieNotification", mock.Anything, int64(99), 1).Return(false, nil)

		// Fetch cast for group card
		cast := []trakt.MovieCastEntry{
			{Character: "Lead", Person: trakt.MoviePerson{Name: "Actor One", IDs: trakt.PersonIDs{IMDB: "nm001"}}},
		}
		traktMock.On("GetMoviePeople", mock.Anything, "movieA").Return(cast, nil)

		// Create movie notification
		store.On("CreateMovieNotification", mock.Anything, mock.AnythingOfType("*storage.MovieNotification")).Return(nil)

		// Look up topics
		store.On("GetTopics", mock.Anything, int64(99)).Return([]storage.Topic{{ThreadID: 42, Name: "movies"}}, nil)

		// createAndFormatWatchStatuses
		store.On("CreateWatchStatusesWithType", mock.Anything, storage.NotificationMovie, uint(0), []uint{uint(0)}).Return(nil)
		store.On("GetWatchStatusesByType", mock.Anything, storage.NotificationMovie, uint(0)).Return([]storage.WatchStatus{
			{Watched: false, User: storage.User{Username: "loraine"}},
		}, nil)

		// UpdateMovieNotificationMessageID via OnSent - not called in test, just needs to exist

		// sendNextTrendingCard for next card (index 1 → movieB)
		// loadChatSettings → GetChatConfig
		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		// GetMovieReleases for next card
		traktMock.On("GetMovieReleases", mock.Anything, "movieB", "us").Return([]trakt.MovieRelease{}, nil)

		// Buffer: callback answer + group post + next card = 3, plus extra for safety
		w := New(store, traktMock, nil, 4)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  0,
			chatID: 99,
		})

		w.handleFollowMovie(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    1,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-follow",
			},
		})

		// First result: callback answer
		toast := <-w.Results()
		assert.Equal(t, "Following!", toast.Text)

		// Second result: group chat post
		groupPost := <-w.Results()
		assert.Equal(t, int64(99), groupPost.ChatID)
		assert.Equal(t, 42, groupPost.ThreadID)
		assert.Contains(t, groupPost.Text, "movieA")
		assert.NotNil(t, groupPost.InlineButtons)

		// Third result: next card (movieB) — edited in the DM
		nextCard := <-w.Results()
		assert.Equal(t, int64(111), nextCard.ChatID)
		assert.Equal(t, 500, nextCard.EditMessageID)
		assert.Contains(t, nextCard.Text, "movieB")

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("skips posting if already in group chat", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("CreateFollowedMovie", mock.Anything, mock.AnythingOfType("*storage.FollowedMovie")).Return(nil)

		// Already posted to group chat
		store.On("HasMovieNotification", mock.Anything, int64(99), 1).Return(true, nil)

		// sendNextTrendingCard for next card
		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		traktMock.On("GetMovieReleases", mock.Anything, "movieB", "us").Return([]trakt.MovieRelease{}, nil)

		w := New(store, traktMock, nil, 3)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  0,
			chatID: 99,
		})

		w.handleFollowMovie(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    1,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-follow",
			},
		})

		// First result: callback answer
		toast := <-w.Results()
		assert.Equal(t, "Following!", toast.Text)

		// Second result: next card (no group post because already exists)
		nextCard := <-w.Results()
		assert.Equal(t, int64(111), nextCard.ChatID)
		assert.Contains(t, nextCard.Text, "movieB")

		// CreateMovieNotification should NOT have been called
		store.AssertNotCalled(t, "CreateMovieNotification")
		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}

func TestHandleSkipMovie(t *testing.T) {
	t.Run("marks movie as seen and advances session", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		user := &storage.User{TelegramID: 111, Username: "loraine"}
		store.On("GetUserByTelegramID", mock.Anything, int64(111)).Return(user, nil)
		store.On("CreateFollowedMovie", mock.Anything, mock.AnythingOfType("*storage.FollowedMovie")).Return(nil)

		// sendNextTrendingCard for next card (index advances to 1)
		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		traktMock.On("GetMovieReleases", mock.Anything, "movieB", "us").Return([]trakt.MovieRelease{}, nil)

		// Buffer: callback answer + next card
		w := New(store, traktMock, nil, 3)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  0,
			chatID: 99,
		})

		w.handleSkipMovie(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    1,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-skip",
			},
		})

		// First result: callback answer
		toast := <-w.Results()
		assert.Equal(t, "Skipped!", toast.Text)

		// Second result: next card (movieB)
		nextCard := <-w.Results()
		assert.Equal(t, int64(111), nextCard.ChatID)
		assert.Equal(t, 500, nextCard.EditMessageID)
		assert.Contains(t, nextCard.Text, "movieB")

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}

func TestHandleMoviePrev(t *testing.T) {
	t.Run("navigates to previous card", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		// sendNextTrendingCard needs GetChatConfig + GetMovieReleases
		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		traktMock.On("GetMovieReleases", mock.Anything, "movieA", "us").Return([]trakt.MovieRelease{}, nil)

		// Buffer: callback answer + edited card
		w := New(store, traktMock, nil, 2)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
			testTrendingMovie(3, "movieC"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  1,
			chatID: 99,
		})

		w.handleMoviePrev(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    2,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-prev",
			},
		})

		// Callback answer
		toast := <-w.Results()
		assert.Equal(t, "", toast.Text) // prev/next have empty callback text
		assert.Equal(t, "cbq-prev", toast.CallbackQueryID)

		// Edited card — should show movieA (index 0)
		card := <-w.Results()
		assert.Contains(t, card.Text, "movieA")
		assert.Equal(t, 500, card.EditMessageID)

		// Verify session index moved to 0
		session := w.getMovieSession(111)
		assert.Equal(t, 0, session.index)

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("does not go below index 0", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		traktMock.On("GetMovieReleases", mock.Anything, "movieA", "us").Return([]trakt.MovieRelease{}, nil)

		w := New(store, traktMock, nil, 2)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  0,
			chatID: 99,
		})

		w.handleMoviePrev(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    1,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-prev",
			},
		})

		<-w.Results() // callback answer
		card := <-w.Results()
		assert.Contains(t, card.Text, "movieA") // still on first card

		session := w.getMovieSession(111)
		assert.Equal(t, 0, session.index)

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}

func TestHandleMovieNext(t *testing.T) {
	t.Run("navigates to next card", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		traktMock.On("GetMovieReleases", mock.Anything, "movieB", "us").Return([]trakt.MovieRelease{}, nil)

		w := New(store, traktMock, nil, 2)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
			testTrendingMovie(3, "movieC"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  0,
			chatID: 99,
		})

		w.handleMovieNext(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    1,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-next",
			},
		})

		// Callback answer
		toast := <-w.Results()
		assert.Equal(t, "", toast.Text)
		assert.Equal(t, "cbq-next", toast.CallbackQueryID)

		// Edited card — should show movieB (index 1)
		card := <-w.Results()
		assert.Contains(t, card.Text, "movieB")
		assert.Equal(t, 500, card.EditMessageID)

		session := w.getMovieSession(111)
		assert.Equal(t, 1, session.index)

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})

	t.Run("does not go past last movie", func(t *testing.T) {
		store := &mocks.MockStore{}
		traktMock := &mocks.MockTrakt{}

		store.On("GetChatConfig", mock.Anything, int64(99)).Return(nil, nil)
		traktMock.On("GetMovieReleases", mock.Anything, "movieC", "us").Return([]trakt.MovieRelease{}, nil)

		w := New(store, traktMock, nil, 2)

		movies := []trakt.TrendingMovie{
			testTrendingMovie(1, "movieA"),
			testTrendingMovie(2, "movieB"),
			testTrendingMovie(3, "movieC"),
		}
		w.setMovieSession(111, &movieBrowseSession{
			movies: movies,
			index:  2,
			chatID: 99,
		})

		w.handleMovieNext(Task{
			ChatID: 42,
			Ctx:    context.Background(),
			Payload: MovieActionPayload{
				TelegramID:      111,
				TraktMovieID:    3,
				ChatID:          42,
				MessageID:       500,
				CallbackQueryID: "cbq-next",
			},
		})

		<-w.Results() // callback answer
		card := <-w.Results()
		assert.Contains(t, card.Text, "movieC") // still on last card

		session := w.getMovieSession(111)
		assert.Equal(t, 2, session.index)

		store.AssertExpectations(t)
		traktMock.AssertExpectations(t)
	})
}

func TestResolveMovieThreadID(t *testing.T) {
	t.Run("returns thread ID for movies topic", func(t *testing.T) {
		topics := []storage.Topic{{ThreadID: 42, Name: "movies"}}
		assert.Equal(t, 42, resolveMovieThreadID(topics))
	})

	t.Run("returns 0 when no movie topic", func(t *testing.T) {
		topics := []storage.Topic{{ThreadID: 10, Name: "anime"}}
		assert.Equal(t, 0, resolveMovieThreadID(topics))
	})

	t.Run("recognizes films as movie topic", func(t *testing.T) {
		topics := []storage.Topic{{ThreadID: 7, Name: "films"}}
		assert.Equal(t, 7, resolveMovieThreadID(topics))
	})
}

func TestHasAvailableRelease(t *testing.T) {
	t.Run("true when digital release is in the past", func(t *testing.T) {
		releases := []trakt.MovieRelease{{ReleaseType: "digital", ReleaseDate: "2020-01-01"}}
		assert.True(t, hasAvailableRelease(releases, time.Now()))
	})

	t.Run("false when only theatrical release exists", func(t *testing.T) {
		releases := []trakt.MovieRelease{{ReleaseType: "theatrical", ReleaseDate: "2020-01-01"}}
		assert.False(t, hasAvailableRelease(releases, time.Now()))
	})

	t.Run("false when digital release is in the future", func(t *testing.T) {
		releases := []trakt.MovieRelease{{ReleaseType: "digital", ReleaseDate: "2099-01-01"}}
		assert.False(t, hasAvailableRelease(releases, time.Now()))
	})

	t.Run("false when no releases", func(t *testing.T) {
		assert.False(t, hasAvailableRelease(nil, time.Now()))
	})
}
