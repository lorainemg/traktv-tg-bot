package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

type chatEpisode struct {
	entry   trakt.CalendarEntry
	userIDs []uint
}

// handleCheckEpisodes fetches all active chats and checks each one for new episodes.
// It iterates per chat (not per user) because notifications are scoped to chats -
// one notification per episode per chat, regardless of how many users follow the show.
func (w *Worker) handleCheckEpisodes(task Task) {
	slog.DebugContext(task.Ctx, "checking for new episodes")

	chatIDs, err := w.store.GetDistinctChatIDs(task.Ctx)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to fetch chat IDs", "error", err)
		return
	}

	today := time.Now().Format("2006-01-02")

	for _, chatID := range chatIDs {
		users, err := w.store.GetUsersByChatID(task.Ctx, chatID)
		if err != nil {
			slog.ErrorContext(task.Ctx, "failed to fetch users for chat", "chat_id", chatID, "error", err)
			continue // non-fatal - skip this chat and try the next one
		}
		w.checkChatEpisodes(task, chatID, users, today)
	}

	slog.DebugContext(task.Ctx, "episode check completed", "chat_count", len(chatIDs))
}

// checkChatEpisodes collects episodes from all users in a chat, deduplicates them,
// and sends one notification per unique episode. Topics and watch providers are
// fetched once per chat rather than per user.
func (w *Worker) checkChatEpisodes(task Task, chatID int64, users []storage.User, day string) {
	settings, err := w.loadChatSettings(task.Ctx, chatID)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to load chat settings", "error", err, "chat_id", chatID)
		return
	}

	// Fetch registered forum topics once for the whole chat
	topics, err := w.store.GetTopics(task.Ctx, chatID)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to fetch topics", "error", err, "chat_id", chatID)
		// Non-fatal - notifications will go to General
		topics = nil
	}

	episodes := w.collectChatEpisodes(task, users, day, settings.notifyHours)

	slog.DebugContext(task.Ctx, "found episodes for chat", "count", len(episodes), "chat_id", chatID)

	// Send a notification for each unique episode
	for _, episode := range episodes {
		var watchInfo *tmdb.WatchInfo
		tmdbID := episode.entry.Show.IDs.TMDB
		if tmdbID != 0 {
			watchInfo, err = w.tmdb.GetWatchProviders(task.Ctx, tmdbID, settings.country)
			if err != nil {
				slog.ErrorContext(task.Ctx, "failed to fetch watch providers", "show", episode.entry.Show.Title, "error", err)
			}
		}
		w.notifyEpisode(task, episode, chatID, topics, watchInfo, settings.location)
	}
}

// collectChatEpisodes fetches calendar entries from every user and merges them
// into a single deduplicated map. The map key "ShowTitle-S02E01" ensures each
// episode appears only once even if multiple users follow the same show.
// notifyHours controls how far ahead to look: only episodes airing within that
// many hours are included.
func (w *Worker) collectChatEpisodes(task Task, users []storage.User, day string, notifyHours int) map[string]chatEpisode {
	episodes := make(map[string]chatEpisode)

	for i := range users {
		user := &users[i] // pointer to slice element — mutations propagate back

		token := w.tokenFor(task.Ctx, user)

		watchlistShows, err := w.trakt.GetWatchlistShows(task.Ctx, token)
		if err != nil {
			slog.ErrorContext(task.Ctx, "failed to fetch watchlist", "user_id", user.ID, "error", err)
			// Non-fatal - proceed without filtering (nil map reads return zero values safely)
			watchlistShows = nil
		}

		// Fetch enough calendar days to cover the notify window.
		// e.g. 12h → 1 day, 36h → 2 days, 48h → 3 days
		calendarDays := notifyHours/24 + 1
		entries, err := w.trakt.GetCalendar(task.Ctx, token, day, calendarDays)
		if err != nil {
			slog.ErrorContext(task.Ctx, "failed to fetch calendar", "user_id", user.ID, "error", err)
			continue
		}

		window := time.Duration(notifyHours) * time.Hour
		for _, entry := range entries {
			if watchlistShows[entry.Show.IDs.Trakt] {
				continue
			}
			// Only notify for episodes airing within the configured window
			if time.Until(entry.FirstAired) > window {
				continue
			}
			key := episodeKey(entry.Show.IDs.Trakt, entry.Episode.Season, entry.Episode.Number)

			if ep, exists := episodes[key]; exists {
				// Episode already in map from another user — just append this user's ID
				ep.userIDs = append(ep.userIDs, user.ID)
				episodes[key] = ep
			} else {
				// First time seeing this episode — create a new entry
				episodes[key] = chatEpisode{
					entry:   entry,
					userIDs: []uint{user.ID},
				}
			}
		}
	}
	return episodes
}

// notifyEpisode checks if an episode was already notified, and if not,
// sends a Result to the output channel and saves the notification.
// topics is the list of registered forum topics for this chat - used to
// route the notification to the right topic thread.
func (w *Worker) notifyEpisode(task Task, episode chatEpisode, chatID int64, topics []storage.Topic, watchInfo *tmdb.WatchInfo, loc *time.Location) {
	entry := episode.entry
	hasNotification, err := w.store.HasNotification(task.Ctx, chatID, entry.Show.Title, entry.Episode.Season, entry.Episode.Number)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to check notification", "error", err, "chat_id", chatID)
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
	if err := w.store.CreateNotification(task.Ctx, &notification); err != nil {
		slog.ErrorContext(task.Ctx, "failed to save notification", "error", err, "chat_id", chatID)
		return
	}

	// Build the full message: episode info + "Watched by" status line
	watchedLine := w.createAndFormatWatchStatuses(task.Ctx, storage.NotificationEpisode, notification.ID, episode.userIDs)
	msg := formatNotificationMessage(&notification, loc)
	if watchedLine != "" {
		msg += "\n\n" + watchedLine
	}

	// Send the message to the results channel for Telegram delivery
	w.results <- Result{
		Ctx:           task.Ctx,
		ChatID:        chatID,
		ThreadID:      threadID,
		Text:          msg,
		PhotoURL:      notification.PhotoURL,
		InlineButtons: watchButtons(storage.NotificationEpisode, notification.ID),
		OnSent: func(messageID int) error {
			return w.store.UpdateNotificationMessageID(task.Ctx, notification.ID, messageID)
		},
	}
}

// createAndFormatWatchStatuses creates WatchStatus rows for every user in the chat
// and returns the formatted "Watched by" line. Returns an empty string if anything
// fails - the notification still goes out, just without the status line.
func (w *Worker) createAndFormatWatchStatuses(ctx context.Context, notificationType storage.NotificationType, notificationID uint, userIDs []uint) string {
	if err := w.store.CreateWatchStatusesWithType(ctx, notificationType, notificationID, userIDs); err != nil {
		slog.ErrorContext(ctx, "failed to create watch statuses", "error", err)
		return ""
	}

	statuses, err := w.store.GetWatchStatusesByType(ctx, notificationType, notificationID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to fetch watch statuses", "error", err)
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
		FirstAired:    entry.FirstAired.Format(time.RFC3339),
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
