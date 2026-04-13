package trakt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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
		httpClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
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
func (c *Client) newRequest(ctx context.Context, method, path, accessToken string, jsonBody []byte) (*http.Request, error) {
	var bodyReader io.Reader
	if jsonBody != nil {
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
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
func (c *Client) do(ctx context.Context, method, path string, token TokenSource, body any) (*http.Response, error) {
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
		req, err := c.newRequest(ctx, method, path, accessToken, jsonBody)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			slog.ErrorContext(ctx, "trakt request failed", "method", method, "path", path, "error", err)
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

		slog.WarnContext(ctx, "trakt rate limited, retrying",
			"method", method, "path", path, "retry_after_secs", retrySec, "attempt", attempt+1, "max_retries", maxRetries)
		time.Sleep(time.Duration(retrySec) * time.Second)
	}

	return nil, fmt.Errorf("trakt API %s %s: still rate limited after %d retries", method, path, maxRetries)
}
