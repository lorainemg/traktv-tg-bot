package worker

import (
	"testing"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/stretchr/testify/assert"
)

func TestFormatTrendingCard(t *testing.T) {
	t.Run("renders full card with all release dates", func(t *testing.T) {
		movie := trakt.TrendingMovie{
			Watchers: 42,
			Movie: trakt.Movie{
				Title:   "The Brutalist",
				Year:    2024,
				IDs:     trakt.MovieIDs{Trakt: 123, Slug: "the-brutalist-2024", IMDB: "tt1234567"},
				Runtime: 215,
				Rating:  8.2,
				Genres:  []string{"drama"},
			},
		}
		releases := []trakt.MovieRelease{
			{ReleaseType: "theatrical", ReleaseDate: "2024-12-20"},
			{ReleaseType: "digital", ReleaseDate: "2025-03-25"},
			{ReleaseType: "physical", ReleaseDate: "2025-04-15"},
		}

		result := formatTrendingCard(movie, releases)

		assert.Contains(t, result, "🎬 *The Brutalist* (2024)")
		assert.Contains(t, result, "_Drama_")
		assert.Contains(t, result, "⏱ 215m")
		assert.Contains(t, result, "[8.2 Trakt](https://trakt.tv/movies/the-brutalist-2024)")
		assert.Contains(t, result, "🎭 Theatrical: Dec 20, 2024")
		assert.Contains(t, result, "📀 Digital: Mar 25, 2025")
		assert.Contains(t, result, "💿 Physical: Apr 15, 2025")
	})

	t.Run("omits missing release dates", func(t *testing.T) {
		movie := trakt.TrendingMovie{
			Movie: trakt.Movie{
				Title:  "Test Movie",
				Year:   2025,
				IDs:    trakt.MovieIDs{Slug: "test-movie-2025"},
				Rating: 7.0,
				Genres: []string{"action", "thriller"},
			},
		}

		result := formatTrendingCard(movie, nil)

		assert.Contains(t, result, "_Action, Thriller_")
		assert.NotContains(t, result, "Theatrical")
		assert.NotContains(t, result, "Digital")
		assert.NotContains(t, result, "Physical")
	})

	t.Run("omits runtime when zero", func(t *testing.T) {
		movie := trakt.TrendingMovie{
			Movie: trakt.Movie{Title: "No Runtime", Year: 2025, IDs: trakt.MovieIDs{Slug: "no-runtime-2025"}},
		}
		result := formatTrendingCard(movie, nil)
		assert.NotContains(t, result, "⏱")
	})
}

func TestFormatMovieNotification(t *testing.T) {
	t.Run("renders group chat card with Stremio link", func(t *testing.T) {
		mn := &storage.MovieNotification{
			MovieTitle: "The Brutalist",
			Year:       2024,
			Genre:      "Drama",
			Runtime:    215,
			Rating:     8.2,
			MovieSlug:  "the-brutalist-2024",
			IMDBID:     "tt1234567",
		}

		result := formatMovieNotification(mn)

		assert.Contains(t, result, "🎬 *The Brutalist* (2024)")
		assert.Contains(t, result, "_Drama_ · ⏱ 215m")
		assert.Contains(t, result, "[8.2 Trakt](https://trakt.tv/movies/the-brutalist-2024)")
		assert.Contains(t, result, "[▶️ Stremio](https://web.strem.io/#/detail/movie/tt1234567)")
	})

	t.Run("omits Stremio when no IMDB ID", func(t *testing.T) {
		mn := &storage.MovieNotification{
			MovieTitle: "No IMDB",
			Year:       2025,
			Genre:      "Comedy",
			MovieSlug:  "no-imdb-2025",
		}

		result := formatMovieNotification(mn)

		assert.NotContains(t, result, "Stremio")
	})
}
