package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
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
		if user.Muted {
			continue // skip users who opted out of notifications
		}
		w.checkUserEpisodes(user, today)
	}
}

// checkUserEpisodes fetches today's calendar for a single user
// and processes each episode entry. Notifications go to user.ChatID —
// the chat where the user authenticated.
func (w *Worker) checkUserEpisodes(user storage.User, day string) {
	// TODO(test): hardcoded Daredevil: Born Again S02E01 — remove after testing reaction feature
	entries := []trakt.CalendarEntry{
		{
			FirstAired: "2026-03-28T02:00:00.000Z",
			Episode:    trakt.Episode{Season: 2, Number: 1, Title: "Test Episode"},
			Show: trakt.Show{
				Title:   "Daredevil: Born Again",
				Genres:  []string{"superhero", "drama", "crime", "action"},
				IDs:     trakt.ShowIDs{Trakt: 195845, Slug: "daredevil-born-again", IMDB: "tt18923754", TMDB: 202555, TVDB: 422712},
				Rating:  7.8,
				Runtime: 53,
				Images:  trakt.ShowImages{Thumb: []string{"media.trakt.tv/images/shows/000/195/845/thumbs/medium/66e362de13.jpg.webp"}},
			},
		},
	}
	fmt.Printf("Found %d episodes for user %d\n", len(entries), user.ID)

	// Fetch the user's watchlist so we can skip shows they haven't started watching
	watchlist, err := w.trakt.GetWatchlistShows(user.TraktAccessToken)
	if err != nil {
		fmt.Println("Error fetching watchlist:", err)
		// Non-fatal — proceed without filtering
		watchlist = nil
	}

	// Fetch registered forum topics for this chat once — avoids hitting
	// the database on every episode in the loop.
	topics, err := w.store.GetTopics(user.ChatID)
	if err != nil {
		fmt.Println("Error fetching topics:", err)
		// Non-fatal — notifications will go to General
		topics = nil
	}

	for _, entry := range entries {
		// Skip shows that are only on the watchlist (not actually watched)
		if watchlist != nil && watchlist[entry.Show.IDs.Trakt] {
			fmt.Printf("Skipping %s — on watchlist only\n", entry.Show.Title)
			continue
		}

		// Fetch watch providers for this show using its TMDB ID.
		// We pass "US" as default — could be made configurable per user later.
		var watchInfo *tmdb.WatchInfo
		if entry.Show.IDs.TMDB != 0 {
			watchInfo, err = w.tmdb.GetWatchProviders(entry.Show.IDs.TMDB, "US")
			if err != nil {
				fmt.Printf("Error fetching watch providers for %s: %v\n", entry.Show.Title, err)
				// Non-fatal — we still send the notification, just without providers
			}
		}

		w.notifyEpisode(entry, user.ChatID, topics, watchInfo)
	}
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
