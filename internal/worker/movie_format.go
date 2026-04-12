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

	// Overview (short synopsis) in italics — last thing on the card
	if movie.Overview != "" {
		overview := escapeMarkdownV1(movie.Overview)
		if len(overview) > 200 {
			overview = overview[:197] + "..."
		}
		msg += fmt.Sprintf("\n\n_%s_", overview)
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

	// Line 3: Trakt rating + Stremio link on same line
	var links []string
	if mn.Rating > 0 && mn.MovieSlug != "" {
		traktURL := fmt.Sprintf("https://trakt.tv/movies/%s", mn.MovieSlug)
		links = append(links, fmt.Sprintf("[%.1f Trakt](%s)", mn.Rating, traktURL))
	}
	if mn.IMDBID != "" {
		stremioURL := fmt.Sprintf("https://web.strem.io/#/detail/movie/%s", mn.IMDBID)
		links = append(links, fmt.Sprintf("[▶️ Stremio](%s)", stremioURL))
	}
	if len(links) > 0 {
		msg += "\n⭐️ " + strings.Join(links, " · ")
	}

	// Overview in italics
	if mn.Overview != "" {
		overview := escapeMarkdownV1(mn.Overview)
		if len(overview) > 200 {
			overview = overview[:197] + "..."
		}
		msg += fmt.Sprintf("\n\n_%s_", overview)
	}

	// Actors line (with empty line before for spacing)
	if mn.Actors != "" {
		msg += fmt.Sprintf("\n\n🎭 %s", mn.Actors)
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

// escapeMarkdownV1 escapes characters that have special meaning in Telegram's
// MarkdownV1 format: underscores (italic), asterisks (bold), backticks (code),
// and square brackets (links). Without escaping, text like "sci_fi" would break
// the italic formatting when wrapped in underscores.
func escapeMarkdownV1(s string) string {
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "[", "\\[")
	return s
}

// formatTopActors builds a comma-separated list of the first N cast members.
// Each actor with an IMDB ID becomes a clickable link; others are plain text.
// Returns empty string if there are no cast members.
func formatTopActors(cast []trakt.MovieCastEntry, max int) string {
	if len(cast) == 0 {
		return ""
	}
	limit := max
	if len(cast) < limit {
		limit = len(cast)
	}
	names := make([]string, limit)
	for i := 0; i < limit; i++ {
		person := cast[i].Person
		if person.IDs.IMDB != "" {
			imdbURL := fmt.Sprintf("https://www.imdb.com/name/%s/", person.IDs.IMDB)
			names[i] = fmt.Sprintf("[%s](%s)", person.Name, imdbURL)
		} else {
			names[i] = person.Name
		}
	}
	return strings.Join(names, ", ")
}

// movieBrowseButtons builds the two-row inline keyboard for trending cards.
// Row 1: Follow + Skip (both mark movie as seen)
// Row 2: Prev + Next (just navigate, no side effects)
// Prev is hidden on the first card, Next is hidden on the last card.
func movieBrowseButtons(traktMovieID int, index, total int) [][]InlineButton {
	// Row 1: Follow and Skip
	row1 := []InlineButton{
		{
			Text:         "✅ Follow",
			CallbackData: fmt.Sprintf("movie_follow:%d", traktMovieID),
		},
		{
			Text:         "⏭ Skip",
			CallbackData: fmt.Sprintf("movie_skip:%d", traktMovieID),
		},
	}

	// Row 2: Prev and Next navigation
	var row2 []InlineButton
	if index > 0 {
		row2 = append(row2, InlineButton{
			Text:         "◀ Prev",
			CallbackData: "movie_prev",
		})
	}
	if index < total-1 {
		row2 = append(row2, InlineButton{
			Text:         "Next ▶",
			CallbackData: "movie_next",
		})
	}

	buttons := [][]InlineButton{row1}
	if len(row2) > 0 {
		buttons = append(buttons, row2)
	}
	return buttons
}
