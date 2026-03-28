package worker

import (
	"fmt"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleCheckEpisodes fetches all linked users and checks each one for new episodes.
func (w *Worker) handleCheckEpisodes(task Task) {
	fmt.Println("Checking for new episodes...")

	users, err := w.store.GetAllUsers()
	if err != nil {
		fmt.Println("Error fetching users:", err)
		return
	}

	today := time.Now().Format("2006-01-02")

	for _, user := range users {
		w.checkUserEpisodes(user, today)
	}
}

// checkUserEpisodes fetches today's calendar for a single user
// and processes each episode entry. Notifications go to user.ChatID —
// the chat where the user authenticated.
func (w *Worker) checkUserEpisodes(user storage.User, day string) {
	entries, err := w.trakt.GetCalendar(user.TraktAccessToken, day, 5)
	if err != nil {
		fmt.Println("Error fetching calendar:", err)
		return
	}
	fmt.Printf("Found %d episodes for user %d\n", len(entries), user.ID)

	for _, entry := range entries {
		episodeKey := fmt.Sprintf("S%02dE%02d", entry.Episode.Season, entry.Episode.Number)
		w.notifyEpisode(user.ID, entry, episodeKey, user.ChatID)
	}
}

// notifyEpisode checks if an episode was already notified, and if not,
// sends a Result to the output channel and saves the notification.
func (w *Worker) notifyEpisode(userID uint, entry trakt.CalendarEntry, episodeKey string, chatID int64) {
	hasNotification, err := w.store.HasNotification(userID, entry.Show.Title, episodeKey)
	if err != nil {
		fmt.Println("Error checking notification:", err)
		return
	}
	if hasNotification {
		return
	}

	// Send the message to the results channel for Telegram delivery
	w.results <- Result{
		ChatID: chatID,
		Text:   formatEpisodeMessage(entry, episodeKey),
	}

	// Record the notification so we don't send it again
	notification := storage.Notification{
		UserID:     userID,
		ShowTitle:  entry.Show.Title,
		EpisodeKey: episodeKey,
	}
	if err := w.store.CreateNotification(&notification); err != nil {
		fmt.Println("Error saving notification:", err)
	}
}

// formatEpisodeMessage builds the notification text for a single episode.
func formatEpisodeMessage(entry trakt.CalendarEntry, episodeKey string) string {
	airDate := formatAirDate(entry.FirstAired)
	return fmt.Sprintf("📺 *%s*\n`%s` - \"_%s_\"\n🗓 %s",
		entry.Show.Title, episodeKey, entry.Episode.Title, airDate)
}

// formatAirDate parses a Trakt ISO timestamp and returns a human-friendly date.
func formatAirDate(isoDate string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", isoDate)
	if err != nil {
		return isoDate
	}
	return t.Format("January 2, 2006 at 3:04 PM")
}
