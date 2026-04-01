package worker

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// unseenShow holds a show and how many episodes the user hasn't watched yet.
type unseenShow struct {
	Show   trakt.Show
	Unseen int
}

// handleUnseen resolves the target user, fetches their watched shows from Trakt,
// computes how many unseen episodes each show has, and sends a summary.
func (w *Worker) handleUnseen(task Task) {
	payload := task.Payload.(UnseenPayload)

	user, err := w.resolveTargetUser(task.Ctx, payload.UserTarget)
	if err != nil {
		slog.Error("unseen: failed to resolve target user", "error", err)
		return
	}
	if user == nil {
		w.results <- task.TextResult("That user hasn't linked their Trakt account yet. They need to /sub first.")
		return
	}

	if user.Muted {
		w.results <- task.TextResult(fmt.Sprintf("%s is currently unsubscribed.", user.MentionLink()))
		return
	}

	entries, err := w.trakt.GetWatchedShows(task.Ctx, w.tokenFor(task.Ctx, user))
	if err != nil {
		slog.Error("unseen: failed to fetch watched shows", "user_id", user.ID, "error", err)
		w.results <- task.TextResult("Failed to fetch shows from Trakt. Try again later.")
		return
	}

	shows := collectUnseenShows(entries)

	if len(shows) == 0 {
		w.results <- task.TextResult(fmt.Sprintf("%s is all caught up! No unseen episodes.", user.MentionLink()))
		return
	}

	w.results <- task.TextResult(formatUnseenMessage(user, shows))
}

// collectUnseenShows filters watched show entries to those with unseen episodes
// and returns them sorted by unseen count (ascending).
func collectUnseenShows(entries []trakt.WatchedShowEntry) []unseenShow {
	unseenShows := make([]unseenShow, 0, len(entries))
	for _, entry := range entries {
		if entry.Plays >= entry.Show.AiredEpisodes {
			continue
		}
		unseenShows = append(unseenShows, unseenShow{
			Show:   entry.Show,
			Unseen: entry.Show.AiredEpisodes - entry.Plays,
		})
	}
	sort.Slice(unseenShows, func(i, j int) bool { return unseenShows[i].Unseen > unseenShows[j].Unseen })
	return unseenShows
}

// formatUnseenMessage builds the Telegram message listing unseen shows for a user.
func formatUnseenMessage(user *storage.User, shows []unseenShow) string {
	header := fmt.Sprintf("📋 *Unseen episodes for* %s\n\n", user.MentionLink())

	lines := make([]string, 0, len(shows))
	for _, s := range shows {
		lines = append(lines, fmt.Sprintf("▸ %s · %d unseen", s.Show.TraktLink(), s.Unseen))
	}
	return header + strings.Join(lines, "\n")
}
