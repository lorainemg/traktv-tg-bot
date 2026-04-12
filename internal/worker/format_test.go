package worker

import (
	"testing"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/stretchr/testify/assert"
)

func TestEpisodeKey(t *testing.T) {
	assert.Equal(t, "1388-02-05", episodeKey(1388, 2, 5))
	assert.Equal(t, "42-10-01", episodeKey(42, 10, 1))
}

func TestFormatEpisodeCode(t *testing.T) {
	assert.Equal(t, "S02E05", formatEpisodeCode(2, 5))
	assert.Equal(t, "S10E01", formatEpisodeCode(10, 1))
}

func TestPaginate(t *testing.T) {
	// Build a slice of 45 items to test pagination with pageSize=20.
	// make([]int, 45) creates a slice of 45 zeroes — we fill it with 1..45.
	items := make([]int, 45)
	for i := range items {
		items[i] = i + 1
	}

	t.Run("first page returns 20 items", func(t *testing.T) {
		page, totalPages := paginate(items, 0) // pages are 0-indexed
		assert.Equal(t, 3, totalPages)
		assert.Len(t, page, 20)
		assert.Equal(t, 1, page[0])
		assert.Equal(t, 20, page[len(page)-1])
	})

	t.Run("last page returns remaining items", func(t *testing.T) {
		page, totalPages := paginate(items, 2)
		assert.Equal(t, 3, totalPages)
		assert.Len(t, page, 5) // 45 - 40 = 5 remaining
		assert.Equal(t, 41, page[0])
	})

	t.Run("out of bounds page returns nil", func(t *testing.T) {
		page, totalPages := paginate(items, 5)
		assert.Nil(t, page)
		assert.Equal(t, 0, totalPages)
	})
}

func TestPaginationButtons(t *testing.T) {
	t.Run("single page returns nil", func(t *testing.T) {
		result := paginationButtons("shows", 0, 1)
		assert.Nil(t, result)
	})

	t.Run("first page has no Prev button", func(t *testing.T) {
		rows := paginationButtons("shows", 0, 3)
		assert.Len(t, rows, 1)

		row := rows[0]
		// First page: only page indicator + Next (no Prev)
		assert.Len(t, row, 2)
		assert.Equal(t, "1/3", row[0].Text)
		assert.Equal(t, "Next ▶", row[1].Text)
		assert.Equal(t, "shows:1", row[1].CallbackData)
	})

	t.Run("middle page has both Prev and Next", func(t *testing.T) {
		rows := paginationButtons("shows", 1, 3)
		row := rows[0]

		assert.Len(t, row, 3) // Prev + indicator + Next
		assert.Equal(t, "◀ Prev", row[0].Text)
		assert.Equal(t, "shows:0", row[0].CallbackData)
		assert.Equal(t, "2/3", row[1].Text)
		assert.Equal(t, "Next ▶", row[2].Text)
		assert.Equal(t, "shows:2", row[2].CallbackData)
	})

	t.Run("last page has no Next button", func(t *testing.T) {
		rows := paginationButtons("shows", 2, 3)

		row := rows[0]
		// Last page: only page indicator + Prev (no Next)
		assert.Len(t, row, 2)
		assert.Equal(t, "◀ Prev", row[0].Text)
		assert.Equal(t, "shows:1", row[0].CallbackData)
		assert.Equal(t, "3/3", row[1].Text)
	})
}

func TestAllWatched(t *testing.T) {
	t.Run("returns true when all statuses are watched", func(t *testing.T) {
		statuses := []storage.WatchStatus{
			{Watched: true},
			{Watched: true},
		}
		assert.True(t, allWatched(statuses))
	})

	t.Run("returns false when any status is unwatched", func(t *testing.T) {
		statuses := []storage.WatchStatus{
			{Watched: true},
			{Watched: false},
		}
		assert.False(t, allWatched(statuses))
	})

	t.Run("returns true for empty list", func(t *testing.T) {
		// An empty range loop never enters the body, so the function
		// returns true — "vacuous truth" (like Python's all([]) == True).
		assert.True(t, allWatched([]storage.WatchStatus{}))
	})
}

func TestFormatStoredProviders(t *testing.T) {
	t.Run("formats providers with links", func(t *testing.T) {
		providers := []storage.ProviderInfo{
			{Name: "Netflix", URL: "https://www.netflix.com"},
			{Name: "Hulu", URL: "https://www.hulu.com"},
		}
		result := formatStoredProviders(providers)
		assert.Equal(t, "[Netflix](https://www.netflix.com) · [Hulu](https://www.hulu.com)", result)
	})

	t.Run("uses plain name when URL is empty", func(t *testing.T) {
		providers := []storage.ProviderInfo{
			{Name: "Netflix", URL: "https://www.netflix.com"},
			{Name: "SomeService"}, // no URL
		}
		result := formatStoredProviders(providers)
		assert.Equal(t, "[Netflix](https://www.netflix.com) · SomeService", result)
	})

	t.Run("filters hidden providers", func(t *testing.T) {
		providers := []storage.ProviderInfo{
			{Name: "Netflix", URL: "https://www.netflix.com"},
			{Name: "Amazon Prime Video with Ads", URL: "https://www.amazon.com"},
		}
		result := formatStoredProviders(providers)
		// "Amazon Prime Video with Ads" is in hiddenProviders — should be excluded
		assert.Equal(t, "[Netflix](https://www.netflix.com)", result)
	})

	t.Run("returns empty string for empty list", func(t *testing.T) {
		assert.Equal(t, "", formatStoredProviders(nil))
	})
}

func TestFormatAirDate(t *testing.T) {
	// time.FixedZone creates a synthetic timezone with a constant offset —
	// unlike time.LoadLocation("America/New_York"), it never shifts for DST,
	// which makes tests deterministic regardless of when they run.
	eastern := time.FixedZone("EST", -5*60*60) // UTC-5

	t.Run("converts UTC to target timezone", func(t *testing.T) {
		// 8pm UTC → 3pm EST (subtract 5 hours)
		result := formatAirDate("2026-03-31T20:00:00Z", eastern)
		assert.Equal(t, "Mar 31 at 3:00 PM EST", result)
	})

	t.Run("returns raw string on invalid input", func(t *testing.T) {
		result := formatAirDate("not-a-date", eastern)
		assert.Equal(t, "not-a-date", result)
	})
}

func TestWatchButtons(t *testing.T) {
	t.Run("episode buttons use e prefix", func(t *testing.T) {
		rows := watchButtons(storage.NotificationEpisode, 42)

		assert.Len(t, rows, 1)
		assert.Len(t, rows[0], 2)
		assert.Equal(t, "✅ Watched", rows[0][0].Text)
		assert.Equal(t, "watched:e:42", rows[0][0].CallbackData)
		assert.Equal(t, "↩️ Unwatched", rows[0][1].Text)
		assert.Equal(t, "unwatched:e:42", rows[0][1].CallbackData)
	})

	t.Run("movie buttons use m prefix", func(t *testing.T) {
		rows := watchButtons(storage.NotificationMovie, 99)

		assert.Len(t, rows, 1)
		assert.Len(t, rows[0], 2)
		assert.Equal(t, "watched:m:99", rows[0][0].CallbackData)
		assert.Equal(t, "unwatched:m:99", rows[0][1].CallbackData)
	})
}

func TestFormatWatchedByLine(t *testing.T) {
	t.Run("returns all caught up when everyone watched", func(t *testing.T) {
		result := formatWatchedByLine(nil, true)
		assert.Equal(t, "All caught up ✅", result)
	})

	t.Run("shows watched and pending icons per user", func(t *testing.T) {
		statuses := []storage.WatchStatus{
			{Watched: true, User: storage.User{Username: "loraine"}},
			{Watched: false, User: storage.User{FirstName: "Bob", TelegramID: 222}},
		}

		result := formatWatchedByLine(statuses, false)

		// loraine watched → ✅, Bob pending → ⏳
		assert.Contains(t, result, "Watched by:")
		assert.Contains(t, result, "@loraine ✅")
		assert.Contains(t, result, "[Bob](tg://user?id=222) ⏳")
	})

	t.Run("all users have watched but no haveAllWatched set", func(t *testing.T) {
		statuses := []storage.WatchStatus{
			{Watched: true, User: storage.User{Username: "loraine"}},
			{Watched: true, User: storage.User{FirstName: "Bob", TelegramID: 222}},
		}

		result := formatWatchedByLine(statuses, false)

		// loraine watched → ✅, Bob pending → ⏳
		assert.Contains(t, result, "Watched by:")
		assert.Contains(t, result, "@loraine ✅")
		assert.Contains(t, result, "[Bob](tg://user?id=222) ✅")
	})
}
