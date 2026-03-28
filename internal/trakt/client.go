package trakt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultBaseURL = "https://api.trakt.tv"
	apiVersion     = "2"
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
	Title   string     `json:"title"`
	Genres  []string   `json:"genres"`  // e.g. ["anime", "drama"] — lowercase strings from extended=full
	IDs     ShowIDs    `json:"ids"`     // external identifiers — populated when using ?extended=full
	Rating  float64    `json:"rating"`  // Trakt community rating (0-10), also from extended=full
	Runtime int        `json:"runtime"` // typical episode length in minutes — from extended=full
	Images  ShowImages `json:"images"`  // poster, fanart, etc. — from extended=full
}

// ShowImages holds image URLs returned by Trakt when using ?extended=full.
// Each field is a slice because Trakt may return multiple URLs per type.
// The URLs are missing the "https://" prefix — we prepend it when using them.
type ShowImages struct {
	Poster []string `json:"poster"`
	Thumb  []string `json:"thumb"`
}

// WatchlistEntry represents one item from the Trakt watchlist API response.
// We only need the show's IDs to build an exclusion set.
type WatchlistEntry struct {
	Show Show `json:"show"`
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
		fmt.Println("Error closing response body:", err)
	}
}

// do execute an HTTP request with Trakt-required headers.
// If the body is non-nil, it gets marshalled to JSON automatically.
// accessToken can be "" for unauthenticated endpoints (like OAuth).
func (c *Client) do(method, path, accessToken string, body any) (*http.Response, error) {
	url := c.baseURL + path

	// If a body is provided, marshal it to JSON. Otherwise, pass nil (no body).
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating trakt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", apiVersion)
	req.Header.Set("trakt-api-key", c.clientID)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	return c.httpClient.Do(req)
}

// GetCalendar fetches upcoming episodes for the user's followed shows.
// Uses: GET /calendars/my/shows/:start_date/:days
func (c *Client) GetCalendar(accessToken, startDate string, days int) ([]CalendarEntry, error) {
	path := fmt.Sprintf("/calendars/my/shows/%s/%d?extended=full", startDate, days)

	resp, err := c.do(http.MethodGet, path, accessToken, nil)
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
func (c *Client) GetWatchlistShows(accessToken string) (map[int]bool, error) {
	resp, err := c.do(http.MethodGet, "/users/me/watchlist/shows", accessToken, nil)
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

	// Build a set using map[int]bool — like a set() in Python
	watchlist := make(map[int]bool, len(entries))
	for _, entry := range entries {
		watchlist[entry.Show.IDs.Trakt] = true
	}

	return watchlist, nil
}

// RequestDeviceCode starts the device auth flow.
// Returns a DeviceCode containing the user_code the user must enter at the verification URL.
func (c *Client) RequestDeviceCode() (*DeviceCode, error) {
	resp, err := c.do(http.MethodPost, "/oauth/device/code", "", struct {
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
	resp, err := c.do(http.MethodPost, "/oauth/device/token", "", struct {
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
