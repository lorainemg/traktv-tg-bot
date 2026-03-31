package worker

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// upcomingEpisode holds a calendar entry together with the list of users
// who follow the show. Unlike collectChatEpisodes (which only deduplicates),
// we need to track *who* watches what so we can display it.
type upcomingEpisode struct {
	Entry trakt.CalendarEntry
	Users []storage.User
}

// handleUpcoming fetches upcoming episodes for all users in the chat,
// groups them by show+episode, and sends a single summary message.
func (w *Worker) handleUpcoming(task Task) {
	chatID := task.ChatID

	// Type-assert the Payload to int. The comma-ok form (value, ok) avoids
	// a panic if the type doesn't match - like a safe cast in C#.
	days, ok := task.Payload.(int)
	if !ok || days < 1 {
		days = 7
	}

	users, err := w.store.GetUsersByChatID(chatID)
	if err != nil {
		slog.Error("upcoming: failed to fetch users", "chat_id", chatID, "error", err)
		return
	}
	if len(users) == 0 {
		w.results <- task.TextResult("No subscribed users in this chat. Use /sub first.")
		return
	}

	settings, err := w.loadChatSettings(chatID)
	if err != nil {
		slog.Error("upcoming: failed to load chat settings", "chat_id", chatID, "error", err)
		return
	}

	today := time.Now().Format("2006-01-02")
	episodes := w.collectUpcomingEpisodes(users, today, days)

	if len(episodes) == 0 {
		w.results <- task.TextResult(fmt.Sprintf("No upcoming episodes in the next %d days.", days))
		return
	}

	page, totalPages := paginate(episodes, 0)
	msg := formatUpcomingMessage(page, settings.location, days, 0, totalPages, len(episodes))

	result := task.TextResult(msg)
	// Callback prefix includes `days` so pagination buttons remember the look-ahead window.
	result.InlineButtons = paginationButtons(fmt.Sprintf("upcoming:%d", days), 0, totalPages)
	w.results <- result
}

// handleUpcomingPage re-fetches upcoming episodes and sends the requested page
// by editing the original message. Triggered by a pagination button click.
func (w *Worker) handleUpcomingPage(task Task) {
	p := task.Payload.(PagePayload)

	w.results <- Result{CallbackQueryID: p.CallbackQueryID}

	users, err := w.store.GetUsersByChatID(task.ChatID)
	if err != nil {
		slog.Error("upcoming page: failed to fetch users", "chat_id", task.ChatID, "error", err)
		return
	}
	if len(users) == 0 {
		return
	}

	settings, err := w.loadChatSettings(task.ChatID)
	if err != nil {
		slog.Error("upcoming page: failed to load chat settings", "chat_id", task.ChatID, "error", err)
		return
	}

	today := time.Now().Format("2006-01-02")
	episodes := w.collectUpcomingEpisodes(users, today, p.Days)
	if len(episodes) == 0 {
		return
	}

	page, totalPages := paginate(episodes, p.Page)
	if page == nil {
		return
	}

	w.results <- Result{
		ChatID:        task.ChatID,
		ThreadID:      task.ThreadID,
		Text:          formatUpcomingMessage(page, settings.location, p.Days, p.Page, totalPages, len(episodes)),
		EditMessageID: p.MessageID,
		InlineButtons: paginationButtons(fmt.Sprintf("upcoming:%d", p.Days), p.Page, totalPages),
	}
}

// collectUpcomingEpisodes fetches calendars from all users and merges them,
// tracking which users follow each episode's show.
func (w *Worker) collectUpcomingEpisodes(users []storage.User, today string, days int) []upcomingEpisode {
	// Map from episode key → upcomingEpisode (entry + list of users)
	episodeMap := make(map[string]*upcomingEpisode)

	for i := range users {
		user := &users[i]

		token := w.tokenFor(user)

		watchlistShows, err := w.trakt.GetWatchlistShows(token)
		if err != nil {
			slog.Error("upcoming: failed to fetch watchlist", "user_id", user.ID, "error", err)
			watchlistShows = nil
		}

		entries, err := w.trakt.GetCalendar(token, today, days)
		if err != nil {
			slog.Error("upcoming: failed to fetch calendar", "user_id", user.ID, "error", err)
			continue
		}

		for _, entry := range entries {
			if watchlistShows[entry.Show.IDs.Trakt] {
				continue
			}
			key := episodeKey(entry.Show.IDs.Trakt, entry.Episode.Season, entry.Episode.Number)
			if existing, ok := episodeMap[key]; ok {
				existing.Users = append(existing.Users, *user)
			} else {
				episodeMap[key] = &upcomingEpisode{
					Entry: entry,
					Users: []storage.User{*user},
				}
			}
		}
	}

	return sortUpcomingEpisodes(episodeMap)
}

// sortUpcomingEpisodes converts the map to a slice sorted by air date (soonest first).
func sortUpcomingEpisodes(episodeMap map[string]*upcomingEpisode) []upcomingEpisode {
	episodes := make([]upcomingEpisode, 0, len(episodeMap))
	for _, ep := range episodeMap {
		episodes = append(episodes, *ep)
	}

	// sort.Slice sorts in-place using a "less" function - like Python's
	// list.sort(key=...) but you provide a comparison function instead of a key.
	// .Before() is the time.Time equivalent of < for timestamps.
	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Entry.FirstAired.Before(episodes[j].Entry.FirstAired)
	})

	return episodes
}

func formatUpcomingMessage(episodes []upcomingEpisode, loc *time.Location, days, page, totalPages, totalEpisodes int) string {
	header := fmt.Sprintf("📅 *Upcoming episodes (%d days)*", days)
	if totalPages > 1 {
		start := page*pageSize + 1
		end := start + len(episodes) - 1
		header += fmt.Sprintf(" _(%d–%d of %d)_", start, end, totalEpisodes)
	}
	header += "\n\n"

	shows := make([]string, 0, len(episodes))
	for _, ep := range episodes {
		users := make([]string, 0, len(ep.Users))
		for _, user := range ep.Users {
			users = append(users, user.MentionLink())
		}

		episodeCode := formatEpisodeCode(ep.Entry.Episode.Season, ep.Entry.Episode.Number)
		shows = append(shows, fmt.Sprintf("▸ %s · %s · in %s\n  👥 %s",
			ep.Entry.Show.TraktLink(), episodeCode, formatTimeUntil(ep.Entry.FirstAired), strings.Join(users, ", ")))
	}

	return header + strings.Join(shows, "\n\n")
}

// formatTimeUntil returns a human-readable string like "2d 5h" or "3h 20m"
// representing how long until the given air date.
func formatTimeUntil(airTime time.Time) string {
	// time.Until returns a Duration - the difference between airTime and now.
	// Like Python's (future - datetime.now()), but returns a single Duration
	// value instead of a timedelta. We break it into days/hours/minutes manually.
	dur := time.Until(airTime)

	if dur < 0 {
		return "aired"
	}

	days := int(dur.Hours()) / 24
	hours := int(dur.Hours()) % 24
	minutes := int(dur.Minutes()) % 60

	switch {
	case days > 0:
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
