package worker

import (
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleCheckEpisodes fetches all active chats and checks each one for new episodes.
// It iterates per chat (not per user) because notifications are scoped to chats -
// one notification per episode per chat, regardless of how many users follow the show.
func (w *Worker) handleCheckEpisodes(task Task) {
	slog.Info("checking for new episodes")

	chatIDs, err := w.store.GetDistinctChatIDs()
	if err != nil {
		slog.Error("failed to fetch chat IDs", "error", err)
		return
	}

	today := time.Now().Format("2006-01-02")

	for _, chatID := range chatIDs {
		users, err := w.store.GetUsersByChatID(chatID)
		if err != nil {
			slog.Error("failed to fetch users for chat", "chat_id", chatID, "error", err)
			continue // non-fatal - skip this chat and try the next one
		}
		w.checkChatEpisodes(chatID, users, today)
	}
}

// checkChatEpisodes collects episodes from all users in a chat, deduplicates them,
// and sends one notification per unique episode. Topics and watch providers are
// fetched once per chat rather than per user.
func (w *Worker) checkChatEpisodes(chatID int64, users []storage.User, day string) {
	settings, err := w.loadChatSettings(chatID)
	if err != nil {
		slog.Error("failed to load chat settings", "error", err, "chat_id", chatID)
		return
	}

	// Fetch registered forum topics once for the whole chat
	topics, err := w.store.GetTopics(chatID)
	if err != nil {
		slog.Error("failed to fetch topics", "error", err)
		// Non-fatal - notifications will go to General
		topics = nil
	}

	episodes := w.collectChatEpisodes(users, day)

	slog.Info("found episodes for chat", "count", len(episodes), "chat_id", chatID)

	// Send a notification for each unique episode
	for _, entry := range episodes {
		var watchInfo *tmdb.WatchInfo
		if entry.Show.IDs.TMDB != 0 {
			watchInfo, err = w.tmdb.GetWatchProviders(entry.Show.IDs.TMDB, settings.country)
			if err != nil {
				slog.Error("failed to fetch watch providers", "show", entry.Show.Title, "error", err)
			}
		}
		w.notifyEpisode(entry, chatID, users, topics, watchInfo, settings.location)
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
			slog.Error("failed to fetch watchlist", "user_id", user.ID, "error", err)
			// Non-fatal - proceed without filtering (nil map reads return zero values safely)
			watchlistShows = nil
		}

		entries, err := w.trakt.GetCalendar(user.TraktAccessToken, day, 5)
		if err != nil {
			slog.Error("failed to fetch calendar", "user_id", user.ID, "error", err)
			continue
		}

		for _, entry := range entries {
			// Skip shows that are only on the watchlist (not actually being watched)
			if watchlistShows[entry.Show.IDs.Trakt] {
				continue
			}
			key := episodeKey(entry.Show.IDs.Trakt, entry.Episode.Season, entry.Episode.Number)
			episodes[key] = entry
		}

	}

	return episodes
}

// notifyEpisode checks if an episode was already notified, and if not,
// sends a Result to the output channel and saves the notification.
// topics is the list of registered forum topics for this chat - used to
// route the notification to the right topic thread.
func (w *Worker) notifyEpisode(entry trakt.CalendarEntry, chatID int64, users []storage.User, topics []storage.Topic, watchInfo *tmdb.WatchInfo, loc *time.Location) {
	hasNotification, err := w.store.HasNotification(chatID, entry.Show.Title, entry.Episode.Season, entry.Episode.Number)
	if err != nil {
		slog.Error("failed to check notification", "error", err)
		return
	}
	if hasNotification {
		return
	}

	// Build the notification record with all episode data stored for future edits.
	notification := buildNotification(entry, chatID, watchInfo)

	// Determine which forum topic to send this episode to
	threadID := resolveThreadID(entry.Show.Genres, topics)

	// Record the notification so we don't send it again
	if err := w.store.CreateNotification(&notification); err != nil {
		slog.Error("failed to save notification", "error", err)
		return
	}

	// Build the full message: episode info + "Watched by" status line
	watchedLine := w.createAndFormatWatchStatuses(notification.ID, users)
	msg := formatNotificationMessage(&notification, loc)
	if watchedLine != "" {
		msg += "\n\n" + watchedLine
	}

	// Send the message to the results channel for Telegram delivery
	w.results <- Result{
		ChatID:        chatID,
		ThreadID:      threadID,
		Text:          msg,
		PhotoURL:      notification.PhotoURL,
		InlineButtons: watchedButton(notification.ID),
		OnSent: func(messageID int) error {
			return w.store.UpdateNotificationMessageID(notification.ID, messageID)
		},
	}
}

// createAndFormatWatchStatuses creates WatchStatus rows for every user in the chat
// and returns the formatted "Watched by" line. Returns an empty string if anything
// fails - the notification still goes out, just without the status line.
func (w *Worker) createAndFormatWatchStatuses(notificationID uint, users []storage.User) string {
	userIDs := make([]uint, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
	}

	if err := w.store.CreateWatchStatuses(notificationID, userIDs); err != nil {
		slog.Error("failed to create watch statuses", "error", err)
		return ""
	}

	statuses, err := w.store.GetWatchStatuses(notificationID)
	if err != nil {
		slog.Error("failed to fetch watch statuses", "error", err)
		return ""
	}

	return formatWatchedByLine(statuses, false)
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

// buildNotification creates a Notification struct from API data, storing all
// fields needed to reconstruct the message later for editing.
func buildNotification(entry trakt.CalendarEntry, chatID int64, watchInfo *tmdb.WatchInfo) storage.Notification {
	// Build thumbnail URL - Trakt returns paths without the protocol prefix
	var photoURL string
	if len(entry.Show.Images.Thumb) > 0 {
		photoURL = "https://" + entry.Show.Images.Thumb[0]
	}

	notification := storage.Notification{
		ChatID:        chatID,
		ShowTitle:     entry.Show.Title,
		Season:        entry.Episode.Season,
		EpisodeNumber: entry.Episode.Number,
		TraktShowID:   entry.Show.IDs.Trakt,
		EpisodeTitle:  entry.Episode.Title,
		FirstAired:    entry.FirstAired,
		Runtime:       entry.Show.Runtime,
		Rating:        entry.Show.Rating,
		ShowSlug:      entry.Show.IDs.Slug,
		IMDBID:        entry.Show.IDs.IMDB,
		PhotoURL:      photoURL,
	}

	// Convert TMDB provider data to our storage type
	if watchInfo != nil {
		notification.WatchLink = watchInfo.Link
		providers := make([]storage.ProviderInfo, len(watchInfo.Providers))
		for i, p := range watchInfo.Providers {
			providers[i] = storage.ProviderInfo{Name: p.Name, URL: p.URL}
		}
		notification.Providers = providers
	}

	return notification
}
