package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleCheckEpisodes fetches all active chats and checks each one for new episodes.
// It iterates per chat (not per user) because notifications are scoped to chats —
// one notification per episode per chat, regardless of how many users follow the show.
func (w *Worker) handleCheckEpisodes(task Task) {
	fmt.Println("Checking for new episodes...")

	chatIDs, err := w.store.GetDistinctChatIDs()
	if err != nil {
		fmt.Println("Error fetching chat IDs:", err)
		return
	}

	today := time.Now().Format("2006-01-02")

	for _, chatID := range chatIDs {
		users, err := w.store.GetUsersByChatID(chatID)
		if err != nil {
			fmt.Printf("Error fetching users for chat %d: %v\n", chatID, err)
			continue // non-fatal — skip this chat and try the next one
		}
		w.checkChatEpisodes(chatID, users, today)
	}
}

// checkChatEpisodes collects episodes from all users in a chat, deduplicates them,
// and sends one notification per unique episode. Topics and watch providers are
// fetched once per chat rather than per user.
func (w *Worker) checkChatEpisodes(chatID int64, users []storage.User, day string) {
	// Fetch registered forum topics once for the whole chat
	topics, err := w.store.GetTopics(chatID)
	if err != nil {
		fmt.Println("Error fetching topics:", err)
		// Non-fatal — notifications will go to General
		topics = nil
	}

	episodes := w.collectChatEpisodes(users, day)

	fmt.Printf("Found %d unique episodes for chat %d\n", len(episodes), chatID)

	// Send a notification for each unique episode
	for _, entry := range episodes {
		var watchInfo *tmdb.WatchInfo
		if entry.Show.IDs.TMDB != 0 {
			watchInfo, err = w.tmdb.GetWatchProviders(entry.Show.IDs.TMDB, "US")
			if err != nil {
				fmt.Printf("Error fetching watch providers for %s: %v\n", entry.Show.Title, err)
			}
		}
		w.notifyEpisode(entry, chatID, topics, watchInfo)
	}
}

// collectChatEpisodes fetches calendar entries from every user and merges them
// into a single deduplicated map. The map key "ShowTitle-S02E01" ensures each
// episode appears only once even if multiple users follow the same show.
func (w *Worker) collectChatEpisodes(users []storage.User, day string) map[string]trakt.CalendarEntry {
	episodes := make(map[string]trakt.CalendarEntry)

	for _, user := range users {
		watchlistShows, err := w.trakt.GetWatchlistShows(user.TraktAccessToken)
		if err != nil {
			fmt.Printf("Error fetching watchlist for user %d: %v\n", user.ID, err)
			// Non-fatal — proceed without filtering (nil map reads return zero values safely)
			watchlistShows = nil
		}

		entries, err := w.trakt.GetCalendar(user.TraktAccessToken, day, 5)
		if err != nil {
			fmt.Printf("Error fetching calendar for user %d: %v\n", user.ID, err)
			continue
		}

		for _, entry := range entries {
			// Skip shows that are only on the watchlist (not actually being watched)
			if watchlistShows[entry.Show.IDs.Trakt] {
				continue
			}
			key := fmt.Sprintf("%s-S%02dE%02d", entry.Show.Title, entry.Episode.Season, entry.Episode.Number)
			episodes[key] = entry
		}
	}

	return episodes
}

// notifyEpisode checks if an episode was already notified, and if not,
// sends a Result to the output channel and saves the notification.
// topics is the list of registered forum topics for this chat — used to
// route the notification to the right topic thread.
func (w *Worker) notifyEpisode(entry trakt.CalendarEntry, chatID int64, topics []storage.Topic, watchInfo *tmdb.WatchInfo) {
	hasNotification, err := w.store.HasNotification(chatID, entry.Show.Title, entry.Episode.Season, entry.Episode.Number)
	if err != nil {
		fmt.Println("Error checking notification:", err)
		return
	}
	if hasNotification {
		return
	}

	// Build the notification record — scoped to the chat, not a specific user.
	// Any user in this chat can later react to mark the episode as watched on their own Trakt account.
	notification := storage.Notification{
		ChatID:        chatID,
		ShowTitle:     entry.Show.Title,
		Season:        entry.Episode.Season,
		EpisodeNumber: entry.Episode.Number,
		TraktShowID:   entry.Show.IDs.Trakt,
	}

	// Build thumbnail URL — Trakt returns paths without the protocol prefix
	var photoURL string
	if len(entry.Show.Images.Thumb) > 0 {
		photoURL = "https://" + entry.Show.Images.Thumb[0]
	}

	// Determine which forum topic to send this episode to
	threadID := resolveThreadID(entry.Show.Genres, topics)

	// Record the notification so we don't send it again
	if err := w.store.CreateNotification(&notification); err != nil {
		fmt.Println("Error saving notification:", err)
	}

	// Send the message to the results channel for Telegram delivery
	w.results <- Result{
		ChatID:   chatID,
		ThreadID: threadID,
		Text:     formatEpisodeMessage(entry, notification.EpisodeKey(), watchInfo),
		PhotoURL: photoURL,
		OnSent: func(messageID int) error {
			return w.store.UpdateNotificationMessageID(notification.ID, messageID)
		},
	}
}

// broadTopicNames lists names that should catch any TV show, not just a specific genre.
// Used as a fallback when no genre-specific topic matches.
var broadTopicNames = map[string]bool{
	"tv shows":  true,
	"tv series": true,
	"shows":     true,
	"series":    true,
	"tv":        true,
}

// resolveThreadID picks the best forum topic thread for an episode based on
// the show's genres and the chat's registered topics.
// Returns 0 (General) if no topics are registered or nothing matches.
func resolveThreadID(genres []string, topics []storage.Topic) int {
	for _, genre := range genres {
		for _, topic := range topics {
			if topic.Name == genre {
				return topic.ThreadID
			}
		}
	}
	for broadName := range broadTopicNames {
		for _, topic := range topics {
			if broadName == topic.Name {
				return topic.ThreadID
			}
		}
	}
	return 0
}

// hiddenProviders lists TMDB provider names we don't want to show in notifications.
var hiddenProviders = map[string]bool{
	"Amazon Prime Video with Ads": true,
}

// formatEpisodeMessage builds the notification text for a single episode.
func formatEpisodeMessage(entry trakt.CalendarEntry, episodeKey string, watchInfo *tmdb.WatchInfo) string {
	airDate := formatAirDate(entry.FirstAired)

	// Line 1: show title + episode key
	msg := fmt.Sprintf("📺 *%s* · %s", entry.Show.Title, episodeKey)

	// Line 2: episode title in italics
	msg += fmt.Sprintf("\n_%s_", entry.Episode.Title)

	// Line 3: date, time, and runtime
	msg += fmt.Sprintf("\n\n🗓 %s", airDate)
	if entry.Show.Runtime > 0 {
		msg += fmt.Sprintf(" · ⏱ %dm", entry.Show.Runtime)
	}

	// Line 4: ratings — Trakt score + IMDb link
	if entry.Show.Rating > 0 || entry.Show.IDs.IMDB != "" {
		var ratings []string
		if entry.Show.Rating > 0 && entry.Show.IDs.Slug != "" {
			traktURL := fmt.Sprintf("https://trakt.tv/shows/%s", entry.Show.IDs.Slug)
			ratings = append(ratings, fmt.Sprintf("[%.1f Trakt](%s)", entry.Show.Rating, traktURL))
		} else if entry.Show.Rating > 0 {
			ratings = append(ratings, fmt.Sprintf("%.1f", entry.Show.Rating))
		}
		if entry.Show.IDs.IMDB != "" {
			imdbURL := fmt.Sprintf("https://www.imdb.com/title/%s/", entry.Show.IDs.IMDB)
			ratings = append(ratings, fmt.Sprintf("[IMDb](%s)", imdbURL))
		}
		msg += "\n⭐️ " + strings.Join(ratings, " · ")
	}

	// Line 4: streaming providers
	if watchInfo != nil && len(watchInfo.Providers) > 0 {
		providerText := formatProviders(watchInfo.Providers)
		if providerText != "" {
			msg += "\n📡 " + providerText
		}
	}

	// Line 5: Stremio + Where to Watch links
	var links []string
	if entry.Show.IDs.IMDB != "" {
		stremioURL := fmt.Sprintf("https://web.strem.io/#/detail/series/%s/%s:%d:%d",
			entry.Show.IDs.IMDB, entry.Show.IDs.IMDB,
			entry.Episode.Season, entry.Episode.Number)
		links = append(links, fmt.Sprintf("[▶️ Stremio](%s)", stremioURL))
	}
	if watchInfo != nil && watchInfo.Link != "" {
		links = append(links, fmt.Sprintf("[🔗 Where to Watch](%s)", watchInfo.Link))
	}
	if len(links) > 0 {
		msg += "\n\n" + strings.Join(links, " · ")
	}

	return msg
}

// formatProviders builds a comma-separated list of streaming services,
// skipping any providers in the hiddenProviders set.
func formatProviders(providers []tmdb.ProviderInfo) string {
	parts := make([]string, 0, len(providers))
	for _, p := range providers {
		if hiddenProviders[p.Name] {
			continue
		}
		if p.URL != "" {
			parts = append(parts, fmt.Sprintf("[%s](%s)", p.Name, p.URL))
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, " · ")
}

// formatAirDate parses a Trakt ISO timestamp (UTC) and returns a human-friendly
// date converted to US Eastern time. time.LoadLocation handles EST/EDT automatically.
func formatAirDate(isoDate string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", isoDate)
	if err != nil {
		return isoDate
	}
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		return t.Format("Jan 2 at 3:04 PM")
	}
	return t.In(eastern).Format("Jan 2 at 3:04 PM EST")
}
