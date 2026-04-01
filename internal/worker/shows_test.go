package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/storage"
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

func TestFormatShowsMessage(t *testing.T) {
	user := &storage.User{FirstName: "Loraine"}
	shows := []trakt.Show{
		{Title: "Severance", IDs: trakt.ShowIDs{Slug: "severance"}},
		{Title: "The Bear", IDs: trakt.ShowIDs{Slug: "the-bear"}},
	}

	t.Run("single page has no range indicator", func(t *testing.T) {
		result := formatShowsMessage(shows, 0, 1, 2, user)

		assert.Contains(t, result, "Loraine's followed shows")
		assert.NotContains(t, result, "of") // no "(1-2 of 2)" for single page
		assert.Contains(t, result, "[Severance](https://trakt.tv/shows/severance)")
		assert.Contains(t, result, "[The Bear](https://trakt.tv/shows/the-bear)")
	})

	t.Run("multi-page shows range indicator", func(t *testing.T) {
		result := formatShowsMessage(shows, 0, 3, 45, user)

		assert.Contains(t, result, "Loraine's followed shows")
		assert.Contains(t, result, "1–2 of 45")
		assert.Contains(t, result, "[The Bear](https://trakt.tv/shows/the-bear)")
	})
}
