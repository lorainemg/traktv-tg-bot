package trakt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testClient creates a Client pointing at a fake HTTP server.
// httptest.NewServer spins up a real localhost server — the client
// talks to it instead of api.trakt.tv, so tests need no network.
func testClient(handler http.HandlerFunc) (*Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := &Client{
		clientID:     "test-client-id",
		clientSecret: "test-secret",
		baseURL:      server.URL,
		httpClient:   server.Client(),
	}
	return client, server
}

func staticToken(token string) TokenSource {
	return func() (string, error) { return token, nil }
}

func TestNewRequest(t *testing.T) {
	c := NewClient("my-api-key", "my-secret")

	t.Run("sets required Trakt headers", func(t *testing.T) {
		req, err := c.newRequest("GET", "/test", "", nil)

		assert.NoError(t, err)
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
		assert.Equal(t, "2", req.Header.Get("trakt-api-version"))
		assert.Equal(t, "my-api-key", req.Header.Get("trakt-api-key"))
		assert.Empty(t, req.Header.Get("Authorization"))
	})

	t.Run("sets Bearer token when provided", func(t *testing.T) {
		req, err := c.newRequest("GET", "/test", "abc123", nil)

		assert.NoError(t, err)
		assert.Equal(t, "Bearer abc123", req.Header.Get("Authorization"))
	})
}

func TestGetCalendar(t *testing.T) {
	t.Run("builds correct URL path with date and days", func(t *testing.T) {
		var capturedPath string

		client, server := testClient(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			json.NewEncoder(w).Encode([]CalendarEntry{})
		})
		defer server.Close()

		_, err := client.GetCalendar(staticToken("token"), "2026-04-01", 7)

		assert.NoError(t, err)
		assert.Equal(t, "/calendars/my/shows/2026-04-01/7", capturedPath)
	})

	t.Run("returns error on non-200 response", func(t *testing.T) {
		client, server := testClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		defer server.Close()

		_, err := client.GetCalendar(staticToken("token"), "2026-04-01", 7)
		assert.Error(t, err)
	})
}

func TestRequestDeviceCode(t *testing.T) {
	t.Run("sends client_id in request body", func(t *testing.T) {
		var capturedBody map[string]string

		client, server := testClient(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			json.NewEncoder(w).Encode(DeviceCode{
				DeviceCode: "abc123",
				UserCode:   "XYZQ",
			})
		})
		defer server.Close()

		_, err := client.RequestDeviceCode()

		assert.NoError(t, err)
		assert.Equal(t, "test-client-id", capturedBody["client_id"])
	})
}