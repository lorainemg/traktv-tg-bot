package trakt

import "context"

// Service defines the Trakt API operations the worker needs.
// The concrete *Client implements all of these. By depending on this
// interface instead of *Client directly, the worker can be tested with
// a mock that returns pre-configured data — no HTTP calls needed.
type Service interface {
	GetCalendar(ctx context.Context, token TokenSource, startDate string, days int) ([]CalendarEntry, error)
	GetWatchlistShows(ctx context.Context, token TokenSource) (map[int]bool, error)
	GetWatchedShows(ctx context.Context, token TokenSource) ([]WatchedShowEntry, error)
	GetWatchHistory(ctx context.Context, token TokenSource, startAt string) ([]HistoryEntry, error)
	SearchShows(ctx context.Context, query string) ([]SearchResult, error)
	MarkEpisodeWatched(ctx context.Context, token TokenSource, traktShowID, season, episodeNumber int) error
	UnmarkEpisodeWatched(ctx context.Context, token TokenSource, traktShowID, season, episodeNumber int) error
	RequestDeviceCode(ctx context.Context) (*DeviceCode, error)
	PollForToken(ctx context.Context, deviceCode string) (*Token, error)
	RefreshToken(ctx context.Context, refreshToken string) (*Token, error)
	GetTrendingMovies(ctx context.Context, limit int) ([]TrendingMovie, error)
	GetMovieReleases(ctx context.Context, movieSlug, country string) ([]MovieRelease, error)
	MarkMovieWatched(ctx context.Context, token TokenSource, traktMovieID int) error
	UnmarkMovieWatched(ctx context.Context, token TokenSource, traktMovieID int) error
}