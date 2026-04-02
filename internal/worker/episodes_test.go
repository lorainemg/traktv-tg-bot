package worker

import (
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
)

func TestResolveThreadID(t *testing.T) {
	// tests is a slice of anonymous structs — each element is one test case.
	// This is Go's equivalent of Python's @pytest.mark.parametrize.
	tests := []struct {
		name     string          // subtest name, shown in test output
		genres   []string        // input: the show's genres
		topics   []storage.Topic // input: the chat's registered topics
		expected int             // expected thread ID result
	}{
		{
			name:   "genre matches topic directly",
			genres: []string{"comedy", "anime"},
			topics: []storage.Topic{
				{ThreadID: 10, Name: "anime"},
				{ThreadID: 20, Name: "drama"},
			},
			expected: 10,
		},
		{
			name:   "falls back to broad topic when no genre matches",
			genres: []string{"comedy", "anime"},
			topics: []storage.Topic{
				{ThreadID: 10, Name: "shows"},
				{ThreadID: 20, Name: "drama"},
			},
			expected: 10,
		},
		{
			name:   "returns 0 when nothing matches",
			genres: []string{"comedy"},
			topics: []storage.Topic{
				{ThreadID: 10, Name: "anime"},
			},
			expected: 0,
		},
		{
			name:   "first genre match",
			genres: []string{"anime", "comedy"},
			topics: []storage.Topic{
				{ThreadID: 10, Name: "comedy"},
				{ThreadID: 20, Name: "anime"},
			},
			expected: 20,
		},
	}

	// Range over the slice — tt is each test case struct.
	// t.Run creates a subtest you can run individually with:
	//   go test -run TestResolveThreadID/"subtest name"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveThreadID(tt.genres, tt.topics)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildNotification(t *testing.T) {
	// Shared test data — a CalendarEntry we can reuse across cases.
	// Defined once outside t.Run so every subtest starts from the same input.
	aired := time.Date(2026, 3, 31, 20, 0, 0, 0, time.UTC)
	entry := trakt.CalendarEntry{
		FirstAired: aired,
		Episode:    trakt.Episode{Season: 2, Number: 5, Title: "The Reckoning"},
		Show: trakt.Show{
			Title:   "Breaking Bad",
			Runtime: 47,
			Rating:  9.2,
			IDs:     trakt.ShowIDs{Trakt: 1388, Slug: "breaking-bad", IMDB: "tt0903747", TMDB: 1396},
			Images:  trakt.ShowImages{Thumb: []string{"walter.trakt.tv/thumb.jpg"}},
		},
	}

	t.Run("with thumbnail and watch providers", func(t *testing.T) {
		watchInfo := &tmdb.WatchInfo{
			Link: "https://www.justwatch.com/show/breaking-bad",
			Providers: []tmdb.ProviderInfo{
				{Name: "Netflix", URL: "https://www.netflix.com"},
			},
		}

		result := buildNotification(entry, 42, watchInfo)

		// Core fields from the entry
		assert.Equal(t, int64(42), result.ChatID)
		assert.Equal(t, "Breaking Bad", result.ShowTitle)
		assert.Equal(t, 2, result.Season)
		assert.Equal(t, 5, result.EpisodeNumber)
		assert.Equal(t, "The Reckoning", result.EpisodeTitle)
		assert.Equal(t, 1388, result.TraktShowID)
		assert.Equal(t, 47, result.Runtime)
		assert.Equal(t, 9.2, result.Rating)

		// Thumbnail gets "https://" prepended
		assert.Equal(t, "https://walter.trakt.tv/thumb.jpg", result.PhotoURL)

		// Watch providers are converted from tmdb types to storage types
		assert.Equal(t, "https://www.justwatch.com/show/breaking-bad", result.WatchLink)
		assert.Len(t, result.Providers, 1)
		assert.Equal(t, "Netflix", result.Providers[0].Name)
	})

	t.Run("without watch providers", func(t *testing.T) {
		var watchInfo *tmdb.WatchInfo

		result := buildNotification(entry, 42, watchInfo)

		assert.Nil(t, result.Providers)
		assert.Equal(t, "", result.WatchLink)
	})
}
