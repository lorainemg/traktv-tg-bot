package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func testClient(handler http.HandlerFunc) (*Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	return &Client{
		apiKey:     "test-key",
		baseURL:    server.URL,
		httpClient: server.Client(),
	}, server
}

func TestGetWatchProviders(t *testing.T) {
	t.Run("returns providers for the requested country with mapped URLs", func(t *testing.T) {
		client, server := testClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "test-key", r.URL.Query().Get("api_key"))
			assert.Contains(t, r.URL.Path, "/tv/1396/watch/providers")

			json.NewEncoder(w).Encode(watchProvidersResponse{
				Results: map[string]CountryProviders{
					"US": {
						Link: "https://www.justwatch.com/us/show/severance",
						Flatrate: []Provider{
							{Name: "Netflix"},
							{Name: "SomeUnknownService"},
						},
					},
				},
			})
		})
		defer server.Close()

		info, err := client.GetWatchProviders(context.Background(), 1396, "US")

		assert.NoError(t, err)
		assert.Equal(t, "https://www.justwatch.com/us/show/severance", info.Link)
		assert.Len(t, info.Providers, 2)
		// Netflix is in providerLinks → gets a homepage URL
		assert.Equal(t, "Netflix", info.Providers[0].Name)
		assert.Equal(t, "https://www.netflix.com", info.Providers[0].URL)
		// Unknown provider → URL stays empty (map zero-value)
		assert.Equal(t, "SomeUnknownService", info.Providers[1].Name)
		assert.Empty(t, info.Providers[1].URL)
	})

	t.Run("returns nil when country not in results", func(t *testing.T) {
		client, server := testClient(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(watchProvidersResponse{
				Results: map[string]CountryProviders{
					"US": {Flatrate: []Provider{{Name: "Netflix"}}},
				},
			})
		})
		defer server.Close()

		// Request GB but only US is available
		info, err := client.GetWatchProviders(context.Background(), 1396, "GB")

		assert.NoError(t, err)
		assert.Nil(t, info) // nil, not an error — "no providers" is a valid result
	})

	t.Run("returns error on non-200 response", func(t *testing.T) {
		client, server := testClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		defer server.Close()

		_, err := client.GetWatchProviders(context.Background(), 1396, "US")
		assert.Error(t, err)
	})
}