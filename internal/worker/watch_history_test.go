package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
)

func TestSyncWatchedEpisodes(t *testing.T) {
	t.Run("matches history entries to unwatched statuses", func(t *testing.T) {
		// Simulate Trakt history: user watched S01E01 and S01E03 of show 100
		history := []trakt.HistoryEntry{
			{
				Show:    trakt.Show{IDs: trakt.ShowIDs{Trakt: 100}},
				Episode: trakt.Episode{Season: 1, Number: 1},
			},
			{
				Show:    trakt.Show{IDs: trakt.ShowIDs{Trakt: 100}},
				Episode: trakt.Episode{Season: 1, Number: 3},
			},
		}

		// Simulate unwatched statuses: S01E01, S01E02, S01E03
		unwatched := []storage.WatchStatus{
			{Notification: storage.Notification{TraktShowID: 100, Season: 1, EpisodeNumber: 1}},
			{Notification: storage.Notification{TraktShowID: 100, Season: 1, EpisodeNumber: 2}},
			{Notification: storage.Notification{TraktShowID: 100, Season: 1, EpisodeNumber: 3}},
		}

		// Only S01E01 and S01E03 appear in history → those two should be returned
		result := syncWatchedEpisodes(history, unwatched)
		assert.Len(t, result, 2)
		assert.Equal(t, 1, result[0].Notification.EpisodeNumber)
		assert.Equal(t, 3, result[1].Notification.EpisodeNumber)
	})

	t.Run("no match between history entries and unwatched statuses", func(t *testing.T) {
		// Simulate Trakt history: user watched S01E01 and S01E03 of show 200
		history := []trakt.HistoryEntry{
			{
				Show:    trakt.Show{IDs: trakt.ShowIDs{Trakt: 200}},
				Episode: trakt.Episode{Season: 1, Number: 1},
			},
			{
				Show:    trakt.Show{IDs: trakt.ShowIDs{Trakt: 200}},
				Episode: trakt.Episode{Season: 1, Number: 3},
			},
		}

		// Simulate unwatched statuses: S01E01, S01E02, S01E03
		unwatched := []storage.WatchStatus{
			{Notification: storage.Notification{TraktShowID: 100, Season: 1, EpisodeNumber: 1}},
			{Notification: storage.Notification{TraktShowID: 100, Season: 1, EpisodeNumber: 2}},
			{Notification: storage.Notification{TraktShowID: 100, Season: 1, EpisodeNumber: 3}},
		}

		// Different show IDs → no overlap → empty result
		result := syncWatchedEpisodes(history, unwatched)
		assert.Empty(t, result)
	})
}
