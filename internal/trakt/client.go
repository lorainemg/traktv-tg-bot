package trakt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://api.trakt.tv"
	apiVersion     = "2"
	maxRetries     = 3 // max times to retry a request after a 429
)

// TokenSource is a function that returns a valid access token, refreshing it
// if necessary. The caller (typically the worker) provides the implementation.
// This keeps token-refresh logic out of the Trakt client — the client just
// calls the function and gets a ready-to-use token.
// Similar to oauth2.TokenSource in Go's standard oauth2 package.
type TokenSource func() (string, error)

// Client handles all communication with the Trakt.tv API.
type Client struct {
	clientID     string       // Trakt API key, sent in every request
	clientSecret string       // Trakt secret, needed for token exchange
	baseURL      string       // API base URL, defaults to https://api.trakt.tv
	httpClient   *http.Client // Go's built-in HTTP client, reusable and concurrency-safe
}

// NewClient creates a Trakt API client.
func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      defaultBaseURL,
		httpClient:   &http.Client{},
	}
}

// closeBody is a helper to safely close an HTTP response body in a deferring.
func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		slog.Error("failed to close response body", "error", err)
	}
}

// newRequest builds an *http.Request with Trakt-required headers.
// accessToken can be "" for unauthenticated endpoints (like OAuth).
func (c *Client) newRequest(method, path, accessToken string, jsonBody []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if jsonBody != nil {
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating trakt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", apiVersion)
	req.Header.Set("trakt-api-key", c.clientID)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	return req, nil
}

// do executes an HTTP request with automatic retry on 429 (rate limited).
// If body is non-nil, it gets marshalled to JSON automatically.
// token can be nil for unauthenticated endpoints (search, OAuth).
func (c *Client) do(method, path string, token TokenSource, body any) (*http.Response, error) {
	// Resolve the access token once, before any retries.
	// If token is nil this is an unauthenticated request — accessToken stays "".
	var accessToken string
	if token != nil {
		var err error
		accessToken, err = token()
		if err != nil {
			return nil, fmt.Errorf("getting access token: %w", err)
		}
	}

	// Marshal once so we can reuse the bytes across retries.
	var jsonBody []byte
	if body != nil {
		var err error
		jsonBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
	}

	for attempt := range maxRetries {
		req, err := c.newRequest(method, path, accessToken, jsonBody)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		closeBody(resp.Body)

		// Default to 10s if Retry-After is missing or unparseable
		retrySec := 10
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if parsed, err := strconv.Atoi(ra); err == nil {
				retrySec = parsed
			}
		}

		slog.Warn("trakt rate limited, retrying",
			"method", method, "path", path, "retry_after_secs", retrySec, "attempt", attempt+1, "max_retries", maxRetries)
		time.Sleep(time.Duration(retrySec) * time.Second)
	}

	return nil, fmt.Errorf("trakt API %s %s: still rate limited after %d retries", method, path, maxRetries)
}

// GetCalendar fetches upcoming episodes for the user's followed shows.
// Uses: GET /calendars/my/shows/:start_date/:days
func (c *Client) GetCalendar(token TokenSource, startDate string, days int) ([]CalendarEntry, error) {
	path := fmt.Sprintf("/calendars/my/shows/%s/%d?extended=full", startDate, days)

	resp, err := c.do(http.MethodGet, path, token, nil)
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
func (c *Client) GetWatchlistShows(token TokenSource) (map[int]bool, error) {
	resp, err := c.do(http.MethodGet, "/users/me/watchlist/shows", token, nil)
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
func (c *Client) GetWatchedShows(token TokenSource) ([]WatchedShowEntry, error) {
	resp, err := c.do(http.MethodGet, "/users/me/watched/shows?extended=full", token, nil)
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
func (c *Client) GetWatchHistory(token TokenSource, startAt string) ([]HistoryEntry, error) {
	path := fmt.Sprintf("/users/me/history/episodes?start_at=%s", startAt)

	resp, err := c.do(http.MethodGet, path, token, nil)
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

// RequestDeviceCode starts the device auth flow.
// Returns a DeviceCode containing the user_code the user must enter at the verification URL.
func (c *Client) RequestDeviceCode() (*DeviceCode, error) {
	resp, err := c.do(http.MethodPost, "/oauth/device/code", nil, struct {
		ClientID string `json:"client_id"`
	}{ClientID: c.clientID})
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("requesting device code: unexpected status %d", resp.StatusCode)
	}

	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("decoding device code response: %w", err)
	}

	return &dc, nil
}

// PollForToken exchanges a device_code for OAuth tokens.
// Returns (nil, nil) if the user hasn't authorized yet (keep polling).
func (c *Client) PollForToken(deviceCode string) (*Token, error) {
	resp, err := c.do(http.MethodPost, "/oauth/device/token", nil, struct {
		DeviceCode   string `json:"code"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}{DeviceCode: deviceCode, ClientID: c.clientID, ClientSecret: c.clientSecret})
	if err != nil {
		return nil, fmt.Errorf("polling for token: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode == http.StatusBadRequest {
		return nil, nil
	}
	if resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("polling for token: code expired")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("polling for token: unexpected status %d", resp.StatusCode)
	}

	var t Token
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &t, nil
}

// RefreshToken exchanges a refresh token for a new access/refresh token pair.
// Uses: POST /oauth/token with grant_type "refresh_token".
// After a successful refresh, the OLD refresh token is invalidated — callers
// must save both the new AccessToken and new RefreshToken.
func (c *Client) RefreshToken(refreshToken string) (*Token, error) {
	resp, err := c.do(http.MethodPost, "/oauth/token", nil, struct {
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURI  string `json:"redirect_uri"`
		GrantType    string `json:"grant_type"`
	}{
		RefreshToken: refreshToken,
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
		RedirectURI:  "urn:ietf:wg:oauth:2.0:oob", // required by Trakt even for device flow apps
		GrantType:    "refresh_token",
	})
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refreshing token: unexpected status %d", resp.StatusCode)
	}

	var t Token
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("decoding refreshed token response: %w", err)
	}
	return &t, nil
}

// SearchShows queries Trakt's search API for shows matching the given text.
// No access token is needed - the search endpoint is public.
// Uses: GET /search/show?query=...&extended=full
func (c *Client) SearchShows(query string) ([]SearchResult, error) {
	// url.QueryEscape encodes the user's text for safe use in a URL,
	// like encodeURIComponent() in JavaScript or urllib.parse.quote() in Python.
	path := fmt.Sprintf("/search/show?query=%s&extended=full&limit=1", url.QueryEscape(query))

	resp, err := c.do(http.MethodGet, path, nil, nil)
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
func (c *Client) MarkEpisodeWatched(token TokenSource, traktShowID, season, episodeNumber int) error {
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

	resp, err := c.do(http.MethodPost, "/sync/history", token, reqBody)
	if err != nil {
		return fmt.Errorf("marking episode watched: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("marking episode watched: unexpected status %d", resp.StatusCode)
	}
	return nil
}
