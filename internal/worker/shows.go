package worker

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// followedShow groups a Trakt show with all users in the chat who follow it.
type followedShow struct {
	Show  trakt.Show
	Users []storage.User
}

// handleShows fetches every authenticated user's watched shows in this chat,
// merges them into a deduplicated "show → users" map, and sends page 0.
func (w *Worker) handleShows(task Task) {
	chatID := task.ChatID

	users, err := w.store.GetUsersByChatID(chatID)
	if err != nil {
		slog.Error("shows: failed to fetch users", "chat_id", chatID, "error", err)
		return
	}
	if len(users) == 0 {
		w.results <- task.TextResult("No authenticated users in this chat. Use /auth first.")
		return
	}

	shows := w.collectFollowedShows(users)

	if len(shows) == 0 {
		w.results <- task.TextResult("No one in this chat is watching any shows yet.")
		return
	}

	page, totalPages := paginate(shows, 0)
	result := task.TextResult(formatShowsMessage(page, 0, totalPages, len(shows)))
	result.InlineButtons = paginationButtons("shows", 0, totalPages)
	w.results <- result
}

// handleShowsPage re-fetches followed shows and sends the requested page
// by editing the original message. Triggered by a pagination button click.
func (w *Worker) handleShowsPage(task Task) {
	p := task.Payload.(PagePayload)

	// Answer the callback query to remove Telegram's loading spinner.
	w.results <- Result{CallbackQueryID: p.CallbackQueryID}

	users, err := w.store.GetUsersByChatID(task.ChatID)
	if err != nil {
		slog.Error("shows page: failed to fetch users", "chat_id", task.ChatID, "error", err)
		return
	}

	shows := w.collectFollowedShows(users)
	if len(shows) == 0 {
		return
	}

	page, totalPages := paginate(shows, p.Page)
	if page == nil {
		return
	}

	w.results <- Result{
		ChatID:        task.ChatID,
		ThreadID:      task.ThreadID,
		Text:          formatShowsMessage(page, p.Page, totalPages, len(shows)),
		EditMessageID: p.MessageID,
		InlineButtons: paginationButtons("shows", p.Page, totalPages),
	}
}

// collectFollowedShows fetches each user's watched shows from Trakt and merges
// them into a deduplicated list sorted alphabetically by show title.
func (w *Worker) collectFollowedShows(users []storage.User) []followedShow {
	// showMap keys on the show title, grouping all users who follow it.
	// Using a map here deduplicates shows across users automatically -
	// like defaultdict(list) in Python.
	showMap := make(map[string]*followedShow)

	for i := range users {
		user := &users[i]

		if err := w.ensureFreshToken(user); err != nil {
			slog.Error("shows: failed to refresh token", "user_id", user.ID, "error", err)
			continue
		}

		entries, err := w.trakt.GetWatchedShows(user.TraktAccessToken)
		if err != nil {
			slog.Error("shows: failed to fetch watched shows", "user_id", user.ID, "error", err)
			continue
		}

		for _, entry := range entries {
			// Skip shows that are no longer airing - only keep "returning series"
			if entry.Show.Status != trakt.ShowStatusReturning {
				continue
			}

			title := entry.Show.Title
			if existing, ok := showMap[title]; ok {
				existing.Users = append(existing.Users, *user)
			} else {
				showMap[title] = &followedShow{
					Show:  entry.Show,
					Users: []storage.User{*user},
				}
			}
		}
	}

	return sortFollowedShows(showMap)
}

// sortFollowedShows converts the map to a slice sorted alphabetically by title.
func sortFollowedShows(showMap map[string]*followedShow) []followedShow {
	shows := make([]followedShow, 0, len(showMap))
	for _, show := range showMap {
		shows = append(shows, *show)
	}

	sort.Slice(shows, func(i, j int) bool {
		return strings.ToLower(shows[i].Show.Title) < strings.ToLower(shows[j].Show.Title)
	})

	return shows
}

// formatShowsMessage builds the Telegram message for a single page of shows.
// When totalPages <= 1, no range is shown in the header.
func formatShowsMessage(shows []followedShow, page, totalPages, totalShows int) string {
	header := "📺 *Followed shows*"
	if totalPages > 1 {
		start := page*pageSize + 1
		end := start + len(shows) - 1
		header += fmt.Sprintf(" _(%d–%d of %d)_", start, end, totalShows)
	}
	header += "\n\n"

	showsMsg := make([]string, 0, len(shows))
	for _, show := range shows {
		users := make([]string, 0, len(show.Users))
		for _, user := range show.Users {
			users = append(users, user.MentionLink())
		}
		showsMsg = append(showsMsg, fmt.Sprintf("▸ %s\n  👥 %s", show.Show.TraktLink(), strings.Join(users, ", ")))
	}
	return header + strings.Join(showsMsg, "\n\n")
}
