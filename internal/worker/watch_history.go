package worker

import (
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleCheckWatchHistory polls Trakt for each user's recent watch history
// and updates TG notifications when an episode was marked as watched on Trakt
// but not yet reflected in the bot's watch status.
func (w *Worker) handleCheckWatchHistory() {
	slog.Info("checking Trakt watch history")

	chatIDs, err := w.store.GetDistinctChatIDs()
	if err != nil {
		slog.Error("failed to fetch chat IDs", "error", err)
		return
	}

	for _, chatID := range chatIDs {
		users, err := w.store.GetUsersByChatID(chatID)
		if err != nil {
			slog.Error("failed to fetch users for chat", "chat_id", chatID, "error", err)
			continue
		}
		for i := range users {
			user := &users[i]
			w.checkUserWatchHistory(user, chatID)
		}
	}
}

// checkUserWatchHistory fetches a single user's recent Trakt watch history,
// compares it against their unwatched notification statuses, and updates
// any matches.
func (w *Worker) checkUserWatchHistory(user *storage.User, chatID int64) {
	unwatched, err := w.store.GetUnwatchedStatusesByUser(user.ID)
	if err != nil {
		slog.Error("failed to fetch unwatched statuses", "user_id", user.ID, "error", err)
		return
	}
	if len(unwatched) == 0 {
		return // nothing pending - skip the API call
	}

	// Look back 2 hours to catch watches since the last poll, with a safety margin
	startAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)

	history, err := w.trakt.GetWatchHistory(w.tokenFor(user), startAt)
	if err != nil {
		slog.Error("failed to fetch watch history", "user_id", user.ID, "error", err)
		return
	}

	matched := syncWatchedEpisodes(history, unwatched)

	for _, status := range matched {
		if err := w.store.MarkWatchStatus(status.Notification.ID, user.ID); err != nil {
			slog.Error("failed to update watch status from history sync", "error", err)
			continue
		}
		w.refreshNotificationMessage(&status.Notification, chatID)
	}
}

// syncWatchedEpisodes cross-references Trakt watch history entries against
// the user's unwatched notification statuses, returning the statuses that
// have a matching entry in the history (i.e. the user watched them on Trakt).
func syncWatchedEpisodes(history []trakt.HistoryEntry, unwatched []storage.WatchStatus) []storage.WatchStatus {
	recentlyWatched := make(map[string]bool, len(history))
	for _, entry := range history {
		recentlyWatched[episodeKey(entry.Show.IDs.Trakt, entry.Episode.Season, entry.Episode.Number)] = true
	}
	watched := make([]storage.WatchStatus, 0, len(unwatched))
	for _, status := range unwatched {
		if recentlyWatched[episodeKey(status.Notification.TraktShowID, status.Notification.Season, status.Notification.EpisodeNumber)] {
			watched = append(watched, status)
		}
	}
	return watched
}
