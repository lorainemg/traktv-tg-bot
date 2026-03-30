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

// handleUpcoming fetches the next 7 days of episodes for all users in the chat,
// groups them by show+episode, and sends a single summary message.
func (w *Worker) handleUpcoming(task Task) {
	chatID := task.ChatID

	users, err := w.store.GetUsersByChatID(chatID)
	if err != nil {
		slog.Error("upcoming: failed to fetch users", "chat_id", chatID, "error", err)
		return
	}
	if len(users) == 0 {
		w.results <- Result{
			ChatID: chatID,
			Text:   "No authenticated users in this chat. Use /auth first.",
		}
		return
	}

	today := time.Now().Format("2006-01-02")
	episodes := w.collectUpcomingEpisodes(users, today)

	if len(episodes) == 0 {
		w.results <- Result{
			ChatID: chatID,
			Text:   "No upcoming episodes in the next 7 days.",
		}
		return
	}

	msg := formatUpcomingMessage(episodes, defaultTimezone)

	w.results <- Result{
		ChatID: chatID,
		Text:   msg,
	}
}

// collectUpcomingEpisodes fetches calendars from all users and merges them,
// tracking which users follow each episode's show.
func (w *Worker) collectUpcomingEpisodes(users []storage.User, today string) []upcomingEpisode {
	// Map from episode key → upcomingEpisode (entry + list of users)
	episodeMap := make(map[string]*upcomingEpisode)

	for _, user := range users {
		watchlistShows, err := w.trakt.GetWatchlistShows(user.TraktAccessToken)
		if err != nil {
			slog.Error("upcoming: failed to fetch watchlist", "user_id", user.ID, "error", err)
			watchlistShows = nil
		}

		entries, err := w.trakt.GetCalendar(user.TraktAccessToken, today, 7)
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
				existing.Users = append(existing.Users, user)
			} else {
				episodeMap[key] = &upcomingEpisode{
					Entry: entry,
					Users: []storage.User{user},
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

	// sort.Slice sorts in-place using a "less" function — like Python's
	// list.sort(key=...) but you provide a comparison function instead of a key.
	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Entry.FirstAired < episodes[j].Entry.FirstAired
	})

	return episodes
}

func formatUpcomingMessage(episodes []upcomingEpisode, loc *time.Location) string {
	header := "📅 *Upcoming episodes*\n\n"

	shows := make([]string, 0, len(episodes))
	for _, ep := range episodes {
		users := make([]string, 0, len(ep.Users))
		for _, user := range ep.Users {
			users = append(users, user.MentionLink())
		}

		episodeCode := formatEpisodeCode(ep.Entry.Episode.Season, ep.Entry.Episode.Number)
		shows = append(shows, fmt.Sprintf("• %s · %s (in %s)\n  %s",
			ep.Entry.Show.TraktLink(), episodeCode, formatTimeUntil(ep.Entry.FirstAired), strings.Join(users, ", ")))
	}

	return header + strings.Join(shows, "\n\n")
}

// formatTimeUntil returns a human-readable string like "2d 5h" or "3h 20m"
// representing how long until the given air date.
func formatTimeUntil(isoDate string) string {
	airTime, err := time.Parse("2006-01-02T15:04:05.000Z", isoDate)
	if err != nil {
		return "?"
	}

	// time.Until returns a Duration — the difference between airTime and now.
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
