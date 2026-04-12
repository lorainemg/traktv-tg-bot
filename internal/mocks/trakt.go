package mocks

import (
	"context"

	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/mock"
)

// MockTrakt is a testify mock for trakt.Service.
type MockTrakt struct {
	mock.Mock
}

func (m *MockTrakt) GetCalendar(ctx context.Context, token trakt.TokenSource, startDate string, days int) ([]trakt.CalendarEntry, error) {
	args := m.Called(ctx, token, startDate, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.CalendarEntry), args.Error(1)
}

func (m *MockTrakt) GetWatchlistShows(ctx context.Context, token trakt.TokenSource) (map[int]bool, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[int]bool), args.Error(1)
}

func (m *MockTrakt) GetWatchedShows(ctx context.Context, token trakt.TokenSource) ([]trakt.WatchedShowEntry, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.WatchedShowEntry), args.Error(1)
}

func (m *MockTrakt) GetWatchHistory(ctx context.Context, token trakt.TokenSource, startAt string) ([]trakt.HistoryEntry, error) {
	args := m.Called(ctx, token, startAt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.HistoryEntry), args.Error(1)
}

func (m *MockTrakt) SearchShows(ctx context.Context, query string) ([]trakt.SearchResult, error) {
	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.SearchResult), args.Error(1)
}

func (m *MockTrakt) MarkEpisodeWatched(ctx context.Context, token trakt.TokenSource, traktShowID, season, episodeNumber int) error {
	return m.Called(ctx, token, traktShowID, season, episodeNumber).Error(0)
}

func (m *MockTrakt) UnmarkEpisodeWatched(ctx context.Context, token trakt.TokenSource, traktShowID, season, episodeNumber int) error {
	return m.Called(ctx, token, traktShowID, season, episodeNumber).Error(0)
}

func (m *MockTrakt) RequestDeviceCode(ctx context.Context) (*trakt.DeviceCode, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trakt.DeviceCode), args.Error(1)
}

func (m *MockTrakt) PollForToken(ctx context.Context, deviceCode string) (*trakt.Token, error) {
	args := m.Called(ctx, deviceCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trakt.Token), args.Error(1)
}

func (m *MockTrakt) RefreshToken(ctx context.Context, refreshToken string) (*trakt.Token, error) {
	args := m.Called(ctx, refreshToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trakt.Token), args.Error(1)
}

func (m *MockTrakt) GetTrendingMovies(ctx context.Context, limit int) ([]trakt.TrendingMovie, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.TrendingMovie), args.Error(1)
}

func (m *MockTrakt) GetMovieReleases(ctx context.Context, movieSlug, country string) ([]trakt.MovieRelease, error) {
	args := m.Called(ctx, movieSlug, country)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.MovieRelease), args.Error(1)
}

func (m *MockTrakt) MarkMovieWatched(ctx context.Context, token trakt.TokenSource, traktMovieID int) error {
	return m.Called(ctx, token, traktMovieID).Error(0)
}

func (m *MockTrakt) UnmarkMovieWatched(ctx context.Context, token trakt.TokenSource, traktMovieID int) error {
	return m.Called(ctx, token, traktMovieID).Error(0)
}
