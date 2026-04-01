package trakt

// Service defines the Trakt API operations the worker needs.
// The concrete *Client implements all of these. By depending on this
// interface instead of *Client directly, the worker can be tested with
// a mock that returns pre-configured data — no HTTP calls needed.
type Service interface {
	GetCalendar(token TokenSource, startDate string, days int) ([]CalendarEntry, error)
	GetWatchlistShows(token TokenSource) (map[int]bool, error)
	GetWatchedShows(token TokenSource) ([]WatchedShowEntry, error)
	GetWatchHistory(token TokenSource, startAt string) ([]HistoryEntry, error)
	SearchShows(query string) ([]SearchResult, error)
	MarkEpisodeWatched(token TokenSource, traktShowID, season, episodeNumber int) error
	UnmarkEpisodeWatched(token TokenSource, traktShowID, season, episodeNumber int) error
	RequestDeviceCode() (*DeviceCode, error)
	PollForToken(deviceCode string) (*Token, error)
	RefreshToken(refreshToken string) (*Token, error)
}