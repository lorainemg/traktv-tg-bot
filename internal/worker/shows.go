package worker

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleShows fetches a single user's followed shows (returning series only)
// and sends page 0 with pagination buttons.
func (w *Worker) handleShows(task Task) {
	target := task.Payload.(UserTarget)

	user, err := w.resolveTargetUser(target)
	if err != nil {
		slog.Error("shows: failed to resolve target user", "error", err)
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

	entries, err := w.trakt.GetWatchedShows(w.tokenFor(user))
	if err != nil {
		slog.Error("shows: failed to fetch watched shows", "user_id", user.ID, "error", err)
		return
	}

	shows := filterReturningShows(entries)
	if len(shows) == 0 {
		w.results <- task.TextResult(fmt.Sprintf("%s isn't watching any returning shows.", user.MentionLink()))
		return
	}

	page, totalPages := paginate(shows, 0)
	// Encode the target user's Telegram ID in the pagination prefix so that
	// page navigation keeps showing the same user's shows.
	prefix := fmt.Sprintf("shows:%d", user.TelegramID)
	result := task.TextResult(formatShowsMessage(page, 0, totalPages, len(shows), user))
	result.InlineButtons = paginationButtons(prefix, 0, totalPages)
	w.results <- result
}

// handleShowsPage re-fetches the target user's shows and sends the requested
// page by editing the original message. Triggered by a pagination button click.
func (w *Worker) handleShowsPage(task Task) {
	p := task.Payload.(PagePayload)

	// Answer the callback query to remove Telegram's loading spinner.
	w.results <- Result{CallbackQueryID: p.CallbackQueryID}

	user, err := w.store.GetUserByTelegramID(p.TargetTelegramID)
	if err != nil || user == nil {
		slog.Error("shows page: failed to fetch user", "telegram_id", p.TargetTelegramID, "error", err)
		return
	}
	if user.Muted {
		return
	}

	entries, err := w.trakt.GetWatchedShows(w.tokenFor(user))
	if err != nil {
		slog.Error("shows page: failed to fetch watched shows", "user_id", user.ID, "error", err)
		return
	}

	shows := filterReturningShows(entries)
	if len(shows) == 0 {
		return
	}

	page, totalPages := paginate(shows, p.Page)
	if page == nil {
		return
	}

	prefix := fmt.Sprintf("shows:%d", user.TelegramID)
	w.results <- Result{
		ChatID:        task.ChatID,
		ThreadID:      task.ThreadID,
		Text:          formatShowsMessage(page, p.Page, totalPages, len(shows), user),
		EditMessageID: p.MessageID,
		InlineButtons: paginationButtons(prefix, p.Page, totalPages),
	}
}

// filterReturningShows keeps only shows with "returning series" status
// and sorts them alphabetically by title.
func filterReturningShows(entries []trakt.WatchedShowEntry) []trakt.Show {
	shows := make([]trakt.Show, 0, len(entries))
	for _, entry := range entries {
		if entry.Show.Status == trakt.ShowStatusReturning {
			shows = append(shows, entry.Show)
		}
	}

	sort.Slice(shows, func(i, j int) bool {
		return strings.ToLower(shows[i].Title) < strings.ToLower(shows[j].Title)
	})

	return shows
}

// formatShowsMessage builds the Telegram message for a single page of a user's shows.
func formatShowsMessage(shows []trakt.Show, page, totalPages, totalShows int, user *storage.User) string {
	header := fmt.Sprintf("📺 *%s's followed shows*", user.FirstName)
	if totalPages > 1 {
		start := page*pageSize + 1
		end := start + len(shows) - 1
		header += fmt.Sprintf(" _(%d–%d of %d)_", start, end, totalShows)
	}
	header += "\n\n"

	lines := make([]string, 0, len(shows))
	for _, show := range shows {
		lines = append(lines, fmt.Sprintf("▸ %s", show.TraktLink()))
	}
	return header + strings.Join(lines, "\n")
}
