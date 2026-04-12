package trakt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GetTrendingMovies fetches movies currently being watched on Trakt.
// No OAuth needed — uses only the API key.
// Uses: GET /movies/trending?limit=N&extended=full
func (c *Client) GetTrendingMovies(ctx context.Context, limit int) ([]TrendingMovie, error) {
	path := fmt.Sprintf("/movies/trending?limit=%d&extended=full", limit)

	resp, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching trending movies: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching trending movies: unexpected status %d", resp.StatusCode)
	}

	var movies []TrendingMovie
	if err := json.NewDecoder(resp.Body).Decode(&movies); err != nil {
		return nil, fmt.Errorf("decoding trending movies response: %w", err)
	}

	return movies, nil
}

// GetMovieReleases fetches release dates for a movie in a specific country.
// No OAuth needed — uses only the API key.
// Uses: GET /movies/{slug}/releases/{country}
func (c *Client) GetMovieReleases(ctx context.Context, movieSlug, country string) ([]MovieRelease, error) {
	path := fmt.Sprintf("/movies/%s/releases/%s", movieSlug, country)

	resp, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching movie releases: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching movie releases: unexpected status %d", resp.StatusCode)
	}

	var releases []MovieRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decoding movie releases response: %w", err)
	}

	return releases, nil
}

// MarkMovieWatched tells Trakt the user has watched a specific movie.
// Uses: POST /sync/history with a movies array.
func (c *Client) MarkMovieWatched(ctx context.Context, token TokenSource, traktMovieID int) error {
	reqBody := MovieSyncHistoryRequest{
		Movies: []MovieSyncEntry{
			{
				IDs:       MovieSyncIDs{Trakt: traktMovieID},
				WatchedAt: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	resp, err := c.do(ctx, http.MethodPost, "/sync/history", token, reqBody)
	if err != nil {
		return fmt.Errorf("marking movie watched: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("marking movie watched: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// GetMoviePeople fetches the cast for a movie.
// No OAuth needed — uses only the API key.
// Uses: GET /movies/{slug}/people
func (c *Client) GetMoviePeople(ctx context.Context, movieSlug string) ([]MovieCastEntry, error) {
	path := fmt.Sprintf("/movies/%s/people", movieSlug)

	resp, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching movie people: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching movie people: unexpected status %d", resp.StatusCode)
	}

	var people MoviePeopleResponse
	if err := json.NewDecoder(resp.Body).Decode(&people); err != nil {
		return nil, fmt.Errorf("decoding movie people response: %w", err)
	}

	return people.Cast, nil
}

// UnmarkMovieWatched removes a movie from the user's Trakt watch history.
// Uses: POST /sync/history/remove with a movies array.
func (c *Client) UnmarkMovieWatched(ctx context.Context, token TokenSource, traktMovieID int) error {
	reqBody := MovieSyncHistoryRequest{
		Movies: []MovieSyncEntry{
			{IDs: MovieSyncIDs{Trakt: traktMovieID}},
		},
	}

	resp, err := c.do(ctx, http.MethodPost, "/sync/history/remove", token, reqBody)
	if err != nil {
		return fmt.Errorf("unmarking movie watched: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unmarking movie watched: unexpected status %d", resp.StatusCode)
	}
	return nil
}