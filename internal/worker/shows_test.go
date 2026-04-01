package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
)

func TestFilterReturningShows(t *testing.T) {
	entries := []trakt.WatchedShowEntry{
		{Show: trakt.Show{Title: "Breaking Bad", Status: trakt.ShowStatusEnded}},
		{Show: trakt.Show{Title: "Severance", Status: trakt.ShowStatusReturning}},
		{Show: trakt.Show{Title: "The Bear", Status: trakt.ShowStatusReturning}},
		{Show: trakt.Show{Title: "Mindhunter", Status: trakt.ShowStatusCanceled}},
	}

	t.Run("keeps only returning shows", func(t *testing.T) {
		result := filterReturningShows(entries)
		assert.Len(t, result, 2)
		// Should only contain the two "returning series" shows
		for _, show := range result {
			assert.Equal(t, trakt.ShowStatusReturning, show.Status)
		}
	})

	t.Run("sorts alphabetically by title", func(t *testing.T) {
		result := filterReturningShows(entries)
		// "Severance" comes before "The Bear" alphabetically
		assert.Equal(t, "Severance", result[0].Title)
		assert.Equal(t, "The Bear", result[1].Title)
	})

	t.Run("returns empty slice when no shows are returning", func(t *testing.T) {
		ended := []trakt.WatchedShowEntry{
			{Show: trakt.Show{Title: "Breaking Bad", Status: trakt.ShowStatusEnded}},
		}
		result := filterReturningShows(ended)
		assert.Empty(t, result)
	})
}
