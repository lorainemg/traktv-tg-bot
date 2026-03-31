package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// defaultTimezone is used when no per-chat timezone is configured.
// Loaded once at package init - safe for concurrent use across goroutines.
// Later, when per-chat timezones are stored in the DB, callers will pass
// the chat's *time.Location instead of this default.
var defaultTimezone = mustLoadLocation("America/New_York")

// mustLoadLocation loads a timezone and panics if it fails - used only for
// the hardcoded default, so a panic means a broken Go installation.
func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("failed to load timezone %q: %v", name, err))
	}
	return loc
}

// pageSize is the maximum number of items shown per page in paginated lists.
const pageSize = 10

// paginate returns the sub-slice for the given page and the total page count.
// [T any] is a type parameter — this single function works for both
// followedShow and upcomingEpisode slices without duplicating code.
func paginate[T any](items []T, page int) ([]T, int) {
	totalPages := (len(items) + pageSize - 1) / pageSize

	if page < 0 || page >= totalPages {
		return nil, 0
	}

	start := page * pageSize
	end := start + pageSize
	// Cap end so the last page doesn't slice past the end of items.
	// Go panics on out-of-bounds slice indices (unlike Python which silently truncates).
	if end > len(items) {
		end = len(items)
	}

	return items[start:end], totalPages
}

// paginationButtons builds a row of ◀ Prev / page indicator / Next ▶ buttons.
// prefix is the callback data prefix (e.g. "shows" or "upcoming:7") — the page
// number is appended as "prefix:page". Returns nil when there's only one page.
func paginationButtons(prefix string, page, totalPages int) [][]InlineButton {
	if totalPages <= 1 {
		return nil
	}
	var row []InlineButton
	if page > 0 {
		row = append(row, InlineButton{
			Text:         "◀ Prev",
			CallbackData: fmt.Sprintf("%s:%d", prefix, page-1),
		})
	}
	// Page indicator — clicking it does nothing (handled as "noop" in the bot).
	row = append(row, InlineButton{
		Text:         fmt.Sprintf("%d/%d", page+1, totalPages),
		CallbackData: "noop",
	})
	if page < totalPages-1 {
		row = append(row, InlineButton{
			Text:         "Next ▶",
			CallbackData: fmt.Sprintf("%s:%d", prefix, page+1),
		})
	}
	return [][]InlineButton{row}
}

// hiddenProviders lists TMDB provider names we don't want to show in notifications.
var hiddenProviders = map[string]bool{
	"Amazon Prime Video with Ads": true,
}

// formatNotificationMessage builds the notification text from stored Notification data.
// Used both for the initial send and when editing the message after a watch status update.
// loc controls the timezone for the air date - pass defaultTimezone when no per-chat setting exists.
func formatNotificationMessage(n *storage.Notification, loc *time.Location) string {
	airDate := formatAirDate(n.FirstAired, loc)

	// Line 1: show title + episode code
	msg := fmt.Sprintf("📺 *%s* · %s", n.ShowTitle, formatEpisodeCode(n.Season, n.EpisodeNumber))

	// Line 2: episode title in italics
	msg += fmt.Sprintf("\n_%s_", n.EpisodeTitle)

	// Line 3: date, time, and runtime
	msg += fmt.Sprintf("\n\n🗓 %s", airDate)
	if n.Runtime > 0 {
		msg += fmt.Sprintf(" · ⏱ %dm", n.Runtime)
	}

	// Line 4: ratings - Trakt score + IMDb link
	if n.Rating > 0 || n.IMDBID != "" {
		var ratings []string
		if n.Rating > 0 && n.ShowSlug != "" {
			traktURL := fmt.Sprintf("https://trakt.tv/shows/%s", n.ShowSlug)
			ratings = append(ratings, fmt.Sprintf("[%.1f Trakt](%s)", n.Rating, traktURL))
		} else if n.Rating > 0 {
			ratings = append(ratings, fmt.Sprintf("%.1f", n.Rating))
		}
		if n.IMDBID != "" {
			imdbURL := fmt.Sprintf("https://www.imdb.com/title/%s/", n.IMDBID)
			ratings = append(ratings, fmt.Sprintf("[IMDb](%s)", imdbURL))
		}
		msg += "\n⭐️ " + strings.Join(ratings, " · ")
	}

	// Line 5: streaming providers
	if len(n.Providers) > 0 {
		providerText := formatStoredProviders(n.Providers)
		if providerText != "" {
			msg += "\n📡 " + providerText
		}
	}

	// Line 6: Stremio + Where to Watch links
	var links []string
	if n.IMDBID != "" {
		stremioURL := fmt.Sprintf("https://web.strem.io/#/detail/series/%s/%s:%d:%d",
			n.IMDBID, n.IMDBID, n.Season, n.EpisodeNumber)
		links = append(links, fmt.Sprintf("[▶️ Stremio](%s)", stremioURL))
	}
	if n.WatchLink != "" {
		links = append(links, fmt.Sprintf("[🔗 Where to Watch](%s)", n.WatchLink))
	}
	if len(links) > 0 {
		msg += "\n\n" + strings.Join(links, " · ")
	}

	return msg
}

// formatStoredProviders builds a comma-separated list of streaming services
// from stored ProviderInfo records, skipping any in the hiddenProviders set.
func formatStoredProviders(providers []storage.ProviderInfo) string {
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

// formatWatchedByLine builds the "Watched by: @user ✅  @other ⏳" status line
// from a list of WatchStatus records (each with a preloaded User).
// This function is also used when editing the message after a user marks an episode as watched.
func formatWatchedByLine(statuses []storage.WatchStatus, haveAllWatched bool) string {
	if haveAllWatched {
		return "All caught up ✅"
	}
	usersInfo := make([]string, len(statuses))
	for i, status := range statuses {
		icon := "⏳"
		if status.Watched {
			icon = "✅"
		}
		usersInfo[i] = fmt.Sprintf("%s %s", status.User.MentionLink(), icon)
	}
	return fmt.Sprintf("Watched by: %s", strings.Join(usersInfo, "  "))
}

// formatAirDate parses a Trakt ISO timestamp (UTC) and returns a human-friendly
// date converted to the given timezone. The loc parameter lets callers pass a
// per-chat timezone; use defaultTimezone when none is configured.
func formatAirDate(isoDate string, loc *time.Location) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", isoDate)
	if err != nil {
		return isoDate
	}
	// t.In(loc) converts from UTC to the target timezone.
	// The "MST" in the format string is a Go layout placeholder - it gets
	// replaced with the timezone abbreviation (EST, EDT, PST, etc.).
	return t.In(loc).Format("Jan 2 at 3:04 PM MST")
}

func episodeKey(traktId, season, episodeNumber int) string {
	return fmt.Sprintf("%d-%02d-%02d", traktId, season, episodeNumber)
}

// formatEpisodeCode returns a human-readable episode code like "S02E05".
func formatEpisodeCode(season, episodeNumber int) string {
	return fmt.Sprintf("S%02dE%02d", season, episodeNumber)
}

// allWatched returns true when every user in the list has marked the episode as watched.
// This is used to decide whether to keep or remove the "Mark as Watched" button.
func allWatched(statuses []storage.WatchStatus) bool {
	for _, s := range statuses {
		if !s.Watched {
			return false
		}
	}
	return true
}

// watchedButton builds a one-row inline keyboard with a "Mark as Watched" button.
// The callback data encodes the notification DB ID so the worker knows which episode
// was clicked. Format: "watched:<id>" - e.g. "watched:42".
func watchedButton(notificationID uint) [][]InlineButton {
	return [][]InlineButton{
		{
			{
				Text:         "✅ Mark as Watched",
				CallbackData: fmt.Sprintf("watched:%d", notificationID),
			},
		},
	}
}
