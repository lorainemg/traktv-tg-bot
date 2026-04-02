package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const baseURL = "https://api.themoviedb.org/3"

// Client handles communication with the TMDB API.
type Client struct {
	apiKey     string       // TMDB API key (v3 auth)
	baseURL    string       // API base URL, defaults to TMDB production
	httpClient *http.Client // reusable HTTP client, same idea as in the Trakt client
}

// NewClient creates a TMDB API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
	}
}

// closeBody safely closes an HTTP response body.
func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		slog.Error("failed to close response body", "error", err)
	}
}

// GetWatchProviders fetches streaming providers for a TV show in a given country.
// tmdbID is the show's TMDB numeric ID, countryCode is e.g. "US", "GB".
// Returns nil (not an error) if no providers are found for that country.
func (c *Client) GetWatchProviders(ctx context.Context, tmdbID int, countryCode string) (*WatchInfo, error) {
	url := fmt.Sprintf("%s/tv/%d/watch/providers?api_key=%s", c.baseURL, tmdbID, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer closeBody(resp.Body) // must defer before the status check so body is always closed

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	var watchProviders watchProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&watchProviders); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}
	providers, ok := watchProviders.Results[countryCode]
	if !ok {
		return nil, nil
	}
	infos := make([]ProviderInfo, 0, len(providers.Flatrate))
	for _, p := range providers.Flatrate {
		// Look up the homepage URL; if not in our map, URL stays empty
		infos = append(infos, ProviderInfo{
			Name: p.Name,
			URL:  providerLinks[p.Name], // returns "" for unknown providers (Go map zero-value)
		})
	}
	watchInfo := &WatchInfo{
		Providers: infos,
		Link:      providers.Link,
	}
	return watchInfo, nil
}
