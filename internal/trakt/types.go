package trakt

import "fmt"

// ShowStatus is a typed string for the status field Trakt returns on shows.
// Using a named type instead of bare string makes valid values discoverable
// and lets the compiler catch typos when comparing - like a string literal
// union type in TypeScript (e.g. type ShowStatus = "returning series" | "ended").
type ShowStatus string

const (
	ShowStatusReturning  ShowStatus = "returning series"
	ShowStatusEnded      ShowStatus = "ended"
	ShowStatusCanceled   ShowStatus = "canceled"
	ShowStatusInProgress ShowStatus = "in production"
)

// CalendarEntry represents one item from the Trakt calendar API response.
type CalendarEntry struct {
	FirstAired string  `json:"first_aired"`
	Episode    Episode `json:"episode"`
	Show       Show    `json:"show"`
}

type Episode struct {
	Season int    `json:"season"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type Show struct {
	Title         string     `json:"title"`
	Status        ShowStatus `json:"status"`         // e.g. "returning series", "ended", "canceled" - from extended=full
	Genres        []string   `json:"genres"`         // e.g. ["anime", "drama"] - lowercase strings from extended=full
	AiredEpisodes int        `json:"aired_episodes"` // total episodes that have aired - from extended=full
	IDs           ShowIDs    `json:"ids"`            // external identifiers - populated when using ?extended=full
	Rating        float64    `json:"rating"`         // Trakt community rating (0-10), also from extended=full
	Runtime       int        `json:"runtime"`        // typical episode length in minutes - from extended=full
	Images        ShowImages `json:"images"`         // poster, fanart, etc. - from extended=full
}

// TraktLink returns a Markdown link to the show's Trakt page,
// e.g. [Breaking Bad](https://trakt.tv/shows/breaking-bad).
func (s Show) TraktLink() string {
	return fmt.Sprintf("[%s](https://trakt.tv/shows/%s)", s.Title, s.IDs.Slug)
}

// ShowImages holds image URLs returned by Trakt when using ?extended=full.
// Each field is a slice because Trakt may return multiple URLs per type.
// The URLs are missing the "https://" prefix - we prepend it when using them.
type ShowImages struct {
	Poster []string `json:"poster"`
	Thumb  []string `json:"thumb"`
}

// WatchlistEntry represents one item from the Trakt watchlist API response.
// We only need the show's IDs to build an exclusion set.
type WatchlistEntry struct {
	Show Show `json:"show"`
}

// WatchedShowEntry represents one item from GET /users/me/watched/shows.
// Unlike WatchlistEntry (shows you plan to watch), this is a show you've
// actually started - Trakt returns it once you've watched at least one episode.
type WatchedShowEntry struct {
	Plays         int    `json:"plays"`           // total episodes watched across all seasons
	LastWatchedAt string `json:"last_watched_at"` // ISO 8601 timestamp of most recent watch
	Show          Show   `json:"show"`
}

// ShowIDs holds the cross-platform identifiers that Trakt returns for a show.
// We need TMDB to call the TMDB watch-providers API later.
type ShowIDs struct {
	Trakt int    `json:"trakt"`
	Slug  string `json:"slug"`
	IMDB  string `json:"imdb"`
	TMDB  int    `json:"tmdb"`
	TVDB  int    `json:"tvdb"`
}

// DeviceCode holds the response from POST /oauth/device/code.
// The user visits VerificationURL and enters UserCode to authorize.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"` // polling interval in seconds
}

// Token holds the OAuth tokens returned after successful authorization.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	CreatedAt    int    `json:"created_at"`
}

// HistoryEntry represents one item from the Trakt watch history API response.
// GET /users/me/history/episodes returns an array of these.
type HistoryEntry struct {
	WatchedAt string  `json:"watched_at"` // ISO 8601 timestamp of when the user watched it
	Episode   Episode `json:"episode"`
	Show      Show    `json:"show"`
}

// SearchResult represents one item from Trakt's search API.
// The API wraps each match in an object with a relevance score and the type
// of media found. We only search for shows, so the Show field is always set.
type SearchResult struct {
	Score float64 `json:"score"` // relevance score - higher is a better match
	Show  Show    `json:"show"`
}

// --- Request types for POST /sync/history ---
// These structs mirror the nested JSON the Trakt API expects:
//   { "shows": [{ "ids": {...}, "seasons": [{ "number": 1, "episodes": [...] }] }] }

// SyncHistoryRequest is the top-level request body for POST /sync/history.
type SyncHistoryRequest struct {
	Shows []SyncShowEntry `json:"shows"`
}

// SyncShowEntry identifies a show and which season/episodes to mark as watched.
type SyncShowEntry struct {
	Ids     SyncShowIDs       `json:"ids"`
	Seasons []SyncSeasonEntry `json:"seasons"`
}

// SyncShowIDs holds the Trakt ID used to identify the show.
type SyncShowIDs struct {
	Trakt int `json:"trakt"`
}

// SyncSeasonEntry groups episodes under a season number.
type SyncSeasonEntry struct {
	Number   int                `json:"number"`
	Episodes []SyncEpisodeEntry `json:"episodes"`
}

// SyncEpisodeEntry marks a single episode as watched at a specific time.
type SyncEpisodeEntry struct {
	Number    int    `json:"number"`
	WatchedAt string `json:"watched_at"` // ISO 8601 timestamp, e.g. "2026-03-28T12:00:00Z"
}
