package trakt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// GetCalendar fetches upcoming episodes for the user's followed shows.
// Uses: GET /calendars/my/shows/:start_date/:days
func (c *Client) GetCalendar(ctx context.Context, token TokenSource, startDate string, days int) ([]CalendarEntry, error) {
	path := fmt.Sprintf("/calendars/my/shows/%s/%d?extended=full", startDate, days)

	resp, err := c.do(ctx, http.MethodGet, path, token, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching calendar: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching calendar: unexpected status %d", resp.StatusCode)
	}

	var entries []CalendarEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding calendar response: %w", err)
	}

	return entries, nil
}

// GetWatchlistShows fetches the user's show watchlist and returns a set of
// Trakt show IDs. This is used to exclude watchlisted (but not watched)
// shows from episode notifications.
func (c *Client) GetWatchlistShows(ctx context.Context, token TokenSource) (map[int]bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/me/watchlist/shows", token, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching watchlist: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching watchlist: unexpected status %d", resp.StatusCode)
	}

	var entries []WatchlistEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding watchlist response: %w", err)
	}

	// Build a set using map[int]bool - like a set() in Python
	watchlist := make(map[int]bool, len(entries))
	for _, entry := range entries {
		watchlist[entry.Show.IDs.Trakt] = true
	}

	return watchlist, nil
}

// GetWatchedShows fetches shows the user has actually started watching
// (at least one episode watched). Returns full show details plus play counts.
// Uses: GET /users/me/watched/shows
func (c *Client) GetWatchedShows(ctx context.Context, token TokenSource) ([]WatchedShowEntry, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/me/watched/shows?extended=full", token, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching watched shows: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching watched shows: unexpected status %d", resp.StatusCode)
	}

	var entries []WatchedShowEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding watched shows response: %w", err)
	}

	return entries, nil
}

// GetWatchHistory fetches the user's recent episode watch history from Trakt.
// startAt limits results to watches after this ISO 8601 timestamp, so we only
// get recent activity instead of the user's entire history.
// Uses: GET /users/me/history/episodes?start_at=...
func (c *Client) GetWatchHistory(ctx context.Context, token TokenSource, startAt string) ([]HistoryEntry, error) {
	path := fmt.Sprintf("/users/me/history/episodes?start_at=%s", startAt)

	resp, err := c.do(ctx, http.MethodGet, path, token, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching watch history: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching watch history: unexpected status %d", resp.StatusCode)
	}

	var entries []HistoryEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding watch history response: %w", err)
	}

	return entries, nil
}

// SearchShows queries Trakt's search API for shows matching the given text.
// No access token is needed - the search endpoint is public.
// Uses: GET /search/show?query=...&extended=full
func (c *Client) SearchShows(ctx context.Context, query string) ([]SearchResult, error) {
	// url.QueryEscape encodes the user's text for safe use in a URL,
	// like encodeURIComponent() in JavaScript or urllib.parse.quote() in Python.
	path := fmt.Sprintf("/search/show?query=%s&extended=full&limit=1", url.QueryEscape(query))

	resp, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("searching shows: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searching shows: unexpected status %d", resp.StatusCode)
	}

	var results []SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decoding search results: %w", err)
	}

	return results, nil
}

// MarkEpisodeWatched tells Trakt the user has watched a specific episode.
// Uses: POST /sync/history - expects 201 Created on success.
func (c *Client) MarkEpisodeWatched(ctx context.Context, token TokenSource, traktShowID, season, episodeNumber int) error {
	// Build the nested request body: shows → seasons → episodes
	reqBody := SyncHistoryRequest{
		Shows: []SyncShowEntry{
			{
				Ids: SyncShowIDs{Trakt: traktShowID},
				Seasons: []SyncSeasonEntry{
					{
						Number: season,
						Episodes: []SyncEpisodeEntry{
							{
								Number:    episodeNumber,
								WatchedAt: time.Now().UTC().Format(time.RFC3339),
							},
						},
					},
				},
			},
		},
	}

	resp, err := c.do(ctx, http.MethodPost, "/sync/history", token, reqBody)
	if err != nil {
		return fmt.Errorf("marking episode watched: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("marking episode watched: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// UnmarkEpisodeWatched removes a specific episode from the user's Trakt watch history.
// Uses: POST /sync/history/remove - the mirror of /sync/history.
// Same request body shape, but returns 200 OK instead of 201 Created.
func (c *Client) UnmarkEpisodeWatched(ctx context.Context, token TokenSource, traktShowID, season, episodeNumber int) error {
	// Reuses the same SyncHistoryRequest structure — Trakt identifies the episode
	// by show ID + season + episode number. WatchedAt is irrelevant for removal.
	reqBody := SyncHistoryRequest{
		Shows: []SyncShowEntry{
			{
				Ids: SyncShowIDs{Trakt: traktShowID},
				Seasons: []SyncSeasonEntry{
					{
						Number:   season,
						Episodes: []SyncEpisodeEntry{{Number: episodeNumber}},
					},
				},
			},
		},
	}

	resp, err := c.do(ctx, http.MethodPost, "/sync/history/remove", token, reqBody)
	if err != nil {
		return fmt.Errorf("unmarking episode watched: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unmarking episode watched: unexpected status %d", resp.StatusCode)
	}
	return nil
}
