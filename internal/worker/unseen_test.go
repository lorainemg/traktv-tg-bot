package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
)

func TestCollectUnseenShows(t *testing.T) {
	entries := []trakt.WatchedShowEntry{
		{Plays: 5, Show: trakt.Show{Title: "Severance", AiredEpisodes: 10}},   // 5 unseen
		{Plays: 20, Show: trakt.Show{Title: "Breaking Bad", AiredEpisodes: 20}}, // caught up
		{Plays: 8, Show: trakt.Show{Title: "The Bear", AiredEpisodes: 10}},     // 2 unseen
		{Plays: 1, Show: trakt.Show{Title: "Shogun", AiredEpisodes: 10}},       // 9 unseen
	}

	t.Run("filters out caught-up shows", func(t *testing.T) {
		result := collectUnseenShows(entries)
		// Breaking Bad (20/20) should be excluded
		assert.Len(t, result, 3)
		for _, show := range result {
			assert.NotEqual(t, "Breaking Bad", show.Show.Title)
		}
	})

	t.Run("calculates correct unseen count", func(t *testing.T) {
		result := collectUnseenShows(entries)
		// Find Severance — 10 aired minus 5 played = 5 unseen
		for _, show := range result {
			if show.Show.Title == "Severance" {
				assert.Equal(t, 5, show.Unseen)
				return
			}
		}
		t.Error("Severance not found in results")
	})

	t.Run("sorts by unseen count descending", func(t *testing.T) {
		result := collectUnseenShows(entries)
		// Shogun (9) > Severance (5) > The Bear (2)
		assert.Equal(t, "Shogun", result[0].Show.Title)
		assert.Equal(t, "Severance", result[1].Show.Title)
		assert.Equal(t, "The Bear", result[2].Show.Title)
	})

	t.Run("returns empty for all caught-up", func(t *testing.T) {
		caughtUp := []trakt.WatchedShowEntry{
			{Plays: 10, Show: trakt.Show{AiredEpisodes: 10}},
		}
		assert.Empty(t, collectUnseenShows(caughtUp))
	})
}

func TestFormatUnseenMessage(t *testing.T) {
	user := &storage.User{Username: "loraine"}
	shows := []unseenShow{
		{Show: trakt.Show{Title: "Severance", IDs: trakt.ShowIDs{Slug: "severance"}}, Unseen: 5},
		{Show: trakt.Show{Title: "The Bear", IDs: trakt.ShowIDs{Slug: "the-bear"}}, Unseen: 2},
	}

	result := formatUnseenMessage(user, shows)

	// Header includes the user's mention link
	assert.Contains(t, result, "[@loraine](https://t.me/loraine)")
	// Each show appears as a Trakt link with unseen count
	assert.Contains(t, result, "[Severance](https://trakt.tv/shows/severance)")
	assert.Contains(t, result, "5 unseen")
	assert.Contains(t, result, "[The Bear](https://trakt.tv/shows/the-bear)")
	assert.Contains(t, result, "2 unseen")
}