package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// hiddenProviders lists TMDB provider names we don't want to show in notifications.
var hiddenProviders = map[string]bool{
	"Amazon Prime Video with Ads": true,
}

// formatNotificationMessage builds the notification text from stored Notification data.
// Used both for the initial send and when editing the message after a watch status update.
func formatNotificationMessage(n *storage.Notification) string {
	airDate := formatAirDate(n.FirstAired)

	// Line 1: show title + episode key
	msg := fmt.Sprintf("📺 *%s* · %s", n.ShowTitle, n.EpisodeKey())

	// Line 2: episode title in italics
	msg += fmt.Sprintf("\n_%s_", n.EpisodeTitle)

	// Line 3: date, time, and runtime
	msg += fmt.Sprintf("\n\n🗓 %s", airDate)
	if n.Runtime > 0 {
		msg += fmt.Sprintf(" · ⏱ %dm", n.Runtime)
	}

	// Line 4: ratings — Trakt score + IMDb link
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
func formatWatchedByLine(statuses []storage.WatchStatus) string {
	usersInfo := make([]string, len(statuses))
	for i, status := range statuses {
		icon := "⏳"
		if status.Watched {
			icon = "✅"
		}
		usersInfo[i] = fmt.Sprintf("%s %s", status.User.MentionLink(), icon)
	}
	return strings.Join(usersInfo, "  ")
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