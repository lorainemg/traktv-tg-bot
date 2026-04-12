package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// releaseTypeEmoji maps Trakt release types to display emojis and labels.
var releaseTypeEmoji = map[string]string{
	"theatrical": "🎭 Theatrical",
	"digital":    "📀 Digital",
	"physical":   "💿 Physical",
	"tv":         "📺 TV",
}

// releaseTypeOrder controls which release types we display and in what order.
var releaseTypeOrder = []string{"theatrical", "digital", "physical", "tv"}

// formatTrendingCard builds the DM card for a trending movie with release dates.
// Used in the paginated browse flow when the weekly ticker fires.
func formatTrendingCard(tm trakt.TrendingMovie, releases []trakt.MovieRelease) string {
	movie := tm.Movie

	// Line 1: title + year
	msg := fmt.Sprintf("🎬 *%s* (%d)", movie.Title, movie.Year)

	// Line 2: genres + runtime
	genreLine := formatMovieGenres(movie.Genres)
	if movie.Runtime > 0 {
		msg += fmt.Sprintf("\n_%s_ · ⏱ %dm", genreLine, movie.Runtime)
	} else if genreLine != "" {
		msg += fmt.Sprintf("\n_%s_", genreLine)
	}

	// Line 3: Trakt rating with link
	if movie.Rating > 0 && movie.IDs.Slug != "" {
		traktURL := fmt.Sprintf("https://trakt.tv/movies/%s", movie.IDs.Slug)
		msg += fmt.Sprintf("\n⭐️ [%.1f Trakt](%s)", movie.Rating, traktURL)
	}

	// Release date lines — only show types that have a date
	releaseLines := formatReleaseLines(releases)
	if releaseLines != "" {
		msg += "\n" + releaseLines
	}

	return msg
}

// formatMovieNotification builds the group chat card posted when a user clicks Follow.
func formatMovieNotification(mn *storage.MovieNotification) string {
	// Line 1: title + year
	msg := fmt.Sprintf("🎬 *%s* (%d)", mn.MovieTitle, mn.Year)

	// Line 2: genre + runtime
	if mn.Runtime > 0 {
		msg += fmt.Sprintf("\n_%s_ · ⏱ %dm", mn.Genre, mn.Runtime)
	} else if mn.Genre != "" {
		msg += fmt.Sprintf("\n_%s_", mn.Genre)
	}

	// Line 3: Trakt rating with link
	if mn.Rating > 0 && mn.MovieSlug != "" {
		traktURL := fmt.Sprintf("https://trakt.tv/movies/%s", mn.MovieSlug)
		msg += fmt.Sprintf("\n⭐️ [%.1f Trakt](%s)", mn.Rating, traktURL)
	}

	// Line 4: Stremio link
	if mn.IMDBID != "" {
		stremioURL := fmt.Sprintf("https://web.strem.io/#/detail/movie/%s", mn.IMDBID)
		msg += fmt.Sprintf("\n[▶️ Stremio](%s)", stremioURL)
	}

	return msg
}

// formatMovieGenres capitalizes and joins genre names for display.
// ["drama", "thriller"] → "Drama, Thriller"
func formatMovieGenres(genres []string) string {
	if len(genres) == 0 {
		return ""
	}
	capitalized := make([]string, len(genres))
	for i, g := range genres {
		if len(g) > 0 {
			capitalized[i] = strings.ToUpper(g[:1]) + g[1:]
		}
	}
	return strings.Join(capitalized, ", ")
}

// formatReleaseLines builds the release date lines for the trending card.
// Only includes release types that have a parseable date.
func formatReleaseLines(releases []trakt.MovieRelease) string {
	// Build a map of release type → earliest date
	byType := make(map[string]string)
	for _, r := range releases {
		if r.ReleaseDate == "" {
			continue
		}
		// Keep the earliest date per type (some countries have multiple theatrical releases)
		if existing, ok := byType[r.ReleaseType]; !ok || r.ReleaseDate < existing {
			byType[r.ReleaseType] = r.ReleaseDate
		}
	}

	var lines []string
	for _, rt := range releaseTypeOrder {
		dateStr, ok := byType[rt]
		if !ok {
			continue
		}
		label, ok := releaseTypeEmoji[rt]
		if !ok {
			continue
		}
		formatted := formatReleaseDate(dateStr)
		lines = append(lines, fmt.Sprintf("%s: %s", label, formatted))
	}
	return strings.Join(lines, "\n")
}

// formatReleaseDate parses a "2024-12-20" date and returns "Dec 20, 2024".
func formatReleaseDate(dateStr string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return t.Format("Jan 2, 2006")
}

// movieFollowSkipButtons builds the Follow/Skip inline keyboard for a trending card.
// traktMovieID is encoded in the Follow callback data so the worker knows which movie.
func movieFollowSkipButtons(traktMovieID int) [][]InlineButton {
	return [][]InlineButton{
		{
			{
				Text:         "✅ Follow",
				CallbackData: fmt.Sprintf("movie_follow:%d", traktMovieID),
			},
			{
				Text:         "⏭ Skip",
				CallbackData: "movie_skip",
			},
		},
	}
}
