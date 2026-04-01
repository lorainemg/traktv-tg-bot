package mocks

import (
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/mock"
)

// MockTrakt is a testify mock for trakt.Service.
type MockTrakt struct {
	mock.Mock
}

func (m *MockTrakt) GetCalendar(token trakt.TokenSource, startDate string, days int) ([]trakt.CalendarEntry, error) {
	args := m.Called(token, startDate, days)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.CalendarEntry), args.Error(1)
}

func (m *MockTrakt) GetWatchlistShows(token trakt.TokenSource) (map[int]bool, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[int]bool), args.Error(1)
}

func (m *MockTrakt) GetWatchedShows(token trakt.TokenSource) ([]trakt.WatchedShowEntry, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.WatchedShowEntry), args.Error(1)
}

func (m *MockTrakt) GetWatchHistory(token trakt.TokenSource, startAt string) ([]trakt.HistoryEntry, error) {
	args := m.Called(token, startAt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.HistoryEntry), args.Error(1)
}

func (m *MockTrakt) SearchShows(query string) ([]trakt.SearchResult, error) {
	args := m.Called(query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]trakt.SearchResult), args.Error(1)
}

func (m *MockTrakt) MarkEpisodeWatched(token trakt.TokenSource, traktShowID, season, episodeNumber int) error {
	return m.Called(token, traktShowID, season, episodeNumber).Error(0)
}

func (m *MockTrakt) UnmarkEpisodeWatched(token trakt.TokenSource, traktShowID, season, episodeNumber int) error {
	return m.Called(token, traktShowID, season, episodeNumber).Error(0)
}

func (m *MockTrakt) RequestDeviceCode() (*trakt.DeviceCode, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trakt.DeviceCode), args.Error(1)
}

func (m *MockTrakt) PollForToken(deviceCode string) (*trakt.Token, error) {
	args := m.Called(deviceCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trakt.Token), args.Error(1)
}

func (m *MockTrakt) RefreshToken(refreshToken string) (*trakt.Token, error) {
	args := m.Called(refreshToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trakt.Token), args.Error(1)
}