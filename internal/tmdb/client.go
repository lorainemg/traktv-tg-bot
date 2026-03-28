package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const baseURL = "https://api.themoviedb.org/3"

// Client handles communication with the TMDB API.
type Client struct {
	apiKey     string       // TMDB API key (v3 auth)
	httpClient *http.Client // reusable HTTP client, same idea as in the Trakt client
}

// NewClient creates a TMDB API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// Provider represents a single streaming service (e.g. Netflix, HBO Max).
type Provider struct {
	Name string `json:"provider_name"`
}

// CountryProviders holds the watch options for a specific country.
type CountryProviders struct {
	Link     string     `json:"link"`     // JustWatch URL for this show in that country
	Flatrate []Provider `json:"flatrate"` // subscription streaming services
}

// watchProvidersResponse is the raw JSON shape from TMDB.
// "results" is a map keyed by country code (e.g. "US", "GB").
type watchProvidersResponse struct {
	Results map[string]CountryProviders `json:"results"`
}

// ProviderInfo holds a streaming service name and its homepage URL (if known).
type ProviderInfo struct {
	Name string // e.g. "Netflix"
	URL  string // e.g. "https://www.netflix.com", empty if unknown
}

// WatchInfo is what we return to callers — a clean summary of where to watch.
type WatchInfo struct {
	Providers []ProviderInfo // streaming services with links
	Link      string         // JustWatch URL for this specific show
}

// providerLinks maps TMDB provider_name values to their homepage URLs.
// Names must match TMDB's exact strings — most use "Plus" spelled out,
// but some like "AMC+" keep the symbol.
var providerLinks = map[string]string{
	"Netflix":                      "https://www.netflix.com",
	"Amazon Prime Video":           "https://www.primevideo.com",
	"Amazon Prime Video with Ads":  "https://www.primevideo.com",
	"Disney Plus":                  "https://www.disneyplus.com",
	"HBO Max":                      "https://www.hbomax.com",
	"Hulu":                         "https://www.hulu.com",
	"Apple TV Plus":                "https://tv.apple.com",
	"Paramount Plus":               "https://www.paramountplus.com",
	"Peacock":                      "https://www.peacocktv.com",
	"Peacock Premium":              "https://www.peacocktv.com",
	"Crunchyroll":                  "https://www.crunchyroll.com",
	"fuboTV":                       "https://www.fubo.tv",
	"YouTube TV":                   "https://tv.youtube.com",
	"Starz":                        "https://www.starz.com",
	"AMC+":                         "https://www.amcplus.com",
}

// closeBody safely closes an HTTP response body.
func closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		fmt.Println("Error closing response body:", err)
	}
}

// GetWatchProviders fetches streaming providers for a TV show in a given country.
// tmdbID is the show's TMDB numeric ID, countryCode is e.g. "US", "GB".
// Returns nil (not an error) if no providers are found for that country.
func (c *Client) GetWatchProviders(tmdbID int, countryCode string) (*WatchInfo, error) {
	url := fmt.Sprintf("%s/tv/%d/watch/providers?api_key=%s", baseURL, tmdbID, c.apiKey)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
