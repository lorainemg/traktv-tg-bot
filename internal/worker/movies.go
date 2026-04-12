package worker

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleSubscribeMovies toggles a movie subscription on or off.
// Used by both /movies and /movies_available — the type is in the payload.
func (w *Worker) handleSubscribeMovies(task Task) {
	payload, ok := task.Payload.(MovieSubscriptionPayload)
	if !ok {
		slog.ErrorContext(task.Ctx, "invalid payload for subscribe movies task")
		return
	}

	user, err := w.store.GetUserByTelegramID(task.Ctx, payload.TelegramID)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to look up user", "error", err)
		w.results <- task.TextResult("Something went wrong, please try again.")
		return
	}
	if user == nil {
		w.results <- task.TextResult("You need to link your Trakt account first. Use /sub.")
		return
	}

	existing, err := w.store.GetMovieSubscription(task.Ctx, user.ID, payload.Type)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to check subscription", "error", err)
		w.results <- task.TextResult("Something went wrong, please try again.")
		return
	}

	label := "trending movies"
	if payload.Type == "available" {
		label = "available trending movies"
	}

	if existing != nil {
		if err := w.store.DeleteMovieSubscription(task.Ctx, user.ID, payload.Type); err != nil {
			slog.ErrorContext(task.Ctx, "failed to delete subscription", "error", err)
			w.results <- task.TextResult("Something went wrong, please try again.")
			return
		}
		w.results <- task.TextResult(fmt.Sprintf("Unsubscribed from %s.", label))
		return
	}

	if err := w.store.CreateMovieSubscription(task.Ctx, &storage.MovieSubscription{
		UserID: user.ID,
		ChatID: payload.ChatID,
		Type:   payload.Type,
	}); err != nil {
		slog.ErrorContext(task.Ctx, "failed to create subscription", "error", err)
		w.results <- task.TextResult("Something went wrong, please try again.")
		return
	}

	w.results <- task.TextResult(fmt.Sprintf("Subscribed to %s! You'll get a list every week.", label))
}

// handleCheckTrendingMovies is triggered by the weekly ticker.
// Fetches trending movies and sends paginated cards to each subscriber's DM.
func (w *Worker) handleCheckTrendingMovies(task Task) {
	slog.DebugContext(task.Ctx, "checking trending movies")

	movies, err := w.trakt.GetTrendingMovies(task.Ctx, 50)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to fetch trending movies", "error", err)
		return
	}

	subscribers, err := w.store.GetMovieSubscribers(task.Ctx)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to fetch movie subscribers", "error", err)
		return
	}

	for _, sub := range subscribers {
		w.sendTrendingToSubscriber(task, sub, movies)
	}

	slog.DebugContext(task.Ctx, "trending movies check completed", "subscriber_count", len(subscribers))
}

// sendTrendingToSubscriber filters movies for a subscriber and starts a browse session.
func (w *Worker) sendTrendingToSubscriber(task Task, sub storage.MovieSubscription, allMovies []trakt.TrendingMovie) {
	// Filter out already-followed/skipped movies
	var filtered []trakt.TrendingMovie
	for _, tm := range allMovies {
		followed, err := w.store.HasFollowedMovie(task.Ctx, sub.UserID, tm.Movie.IDs.Trakt)
		if err != nil {
			slog.ErrorContext(task.Ctx, "failed to check followed movie", "error", err)
			continue
		}
		if followed {
			continue
		}
		filtered = append(filtered, tm)
	}

	// For "available" subscriptions, filter to movies with past digital/physical release
	if sub.Type == "available" {
		filtered = w.filterAvailableMovies(task, sub, filtered)
	}

	if len(filtered) == 0 {
		w.results <- Result{
			Ctx:    task.Ctx,
			ChatID: sub.User.TelegramID,
			Text:   "No new trending movies this week!",
		}
		return
	}

	// Store the session and send the first card
	session := &movieBrowseSession{
		movies: filtered,
		index:  0,
		chatID: sub.ChatID,
	}
	w.setMovieSession(sub.User.TelegramID, session)
	w.sendNextTrendingCard(task, sub.User.TelegramID, session, 0)
}

// filterAvailableMovies keeps only movies with a past digital or physical release date.
func (w *Worker) filterAvailableMovies(task Task, sub storage.MovieSubscription, movies []trakt.TrendingMovie) []trakt.TrendingMovie {
	// Load country from the group chat config
	settings, err := w.loadChatSettings(task.Ctx, sub.ChatID)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to load chat settings", "error", err, "chat_id", sub.ChatID)
		// Fall back: return all movies rather than filtering with no country
		return movies
	}

	now := time.Now()
	// Trakt expects lowercase country codes in URLs (e.g. "us" not "US")
	country := strings.ToLower(settings.country)
	var available []trakt.TrendingMovie
	for _, tm := range movies {
		releases, err := w.trakt.GetMovieReleases(task.Ctx, tm.Movie.IDs.Slug, country)
		if err != nil {
			slog.ErrorContext(task.Ctx, "failed to fetch releases", "error", err, "movie", tm.Movie.Title)
			continue
		}
		if hasAvailableRelease(releases, now) {
			available = append(available, tm)
		}
	}
	return available
}

// hasAvailableRelease checks if any digital or physical release date is in the past.
func hasAvailableRelease(releases []trakt.MovieRelease, now time.Time) bool {
	for _, r := range releases {
		if r.ReleaseType != "digital" && r.ReleaseType != "physical" {
			continue
		}
		releaseDate, err := time.Parse("2006-01-02", r.ReleaseDate)
		if err != nil {
			continue
		}
		if releaseDate.Before(now) {
			return true
		}
	}
	return false
}

// sendNextTrendingCard sends or edits the current movie card from the browse session.
// editMessageID: 0 = send new message (first card), non-zero = edit existing message.
// If all movies are shown, sends/edits a completion message and cleans up.
func (w *Worker) sendNextTrendingCard(task Task, telegramID int64, session *movieBrowseSession, editMessageID int) {
	if session.index >= len(session.movies) {
		w.clearMovieSession(telegramID)
		w.results <- Result{
			Ctx:           task.Ctx,
			ChatID:        telegramID,
			EditMessageID: editMessageID,
			Text:          "That's all for this week! 🎬",
		}
		return
	}

	tm := session.movies[session.index]

	// Fetch release dates for this movie
	settings, _ := w.loadChatSettings(task.Ctx, session.chatID)
	country := "us"
	if settings.country != "" {
		// Trakt expects lowercase country codes in URLs (e.g. "us" not "US")
		country = strings.ToLower(settings.country)
	}
	releases, err := w.trakt.GetMovieReleases(task.Ctx, tm.Movie.IDs.Slug, country)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to fetch releases for card", "error", err, "movie", tm.Movie.Title)
		// Non-fatal — show card without release dates
		releases = nil
	}

	msg := formatTrendingCard(tm, releases)

	// Build thumbnail URL - Trakt returns paths without the protocol prefix
	var photoURL string
	if len(tm.Movie.Images.Thumb) > 0 {
		photoURL = "https://" + tm.Movie.Images.Thumb[0]
	}

	w.results <- Result{
		Ctx:           task.Ctx,
		ChatID:        telegramID,
		EditMessageID: editMessageID,
		Text:          msg,
		PhotoURL:      photoURL,
		InlineButtons: movieBrowseButtons(tm.Movie.IDs.Trakt, session.index, len(session.movies)),
	}
}

// handleFollowMovie posts a movie to the group chat and advances the browse session.
func (w *Worker) handleFollowMovie(task Task) {
	payload, ok := task.Payload.(MovieActionPayload)
	if !ok {
		slog.ErrorContext(task.Ctx, "invalid payload for follow movie task")
		return
	}

	user, err := w.store.GetUserByTelegramID(task.Ctx, payload.TelegramID)
	if err != nil || user == nil {
		slog.ErrorContext(task.Ctx, "failed to look up user for follow", "error", err)
		return
	}

	// Save as followed (dedup for next week)
	if err := w.store.CreateFollowedMovie(task.Ctx, &storage.FollowedMovie{
		UserID:       user.ID,
		TraktMovieID: payload.TraktMovieID,
	}); err != nil {
		slog.ErrorContext(task.Ctx, "failed to save followed movie", "error", err)
		// Non-fatal — continue with posting
	}

	// Answer the callback query
	w.answerCallback(task.Ctx, payload.CallbackQueryID, "Following!", false)

	// Post to group chat if not already posted
	session := w.getMovieSession(payload.TelegramID)
	if session == nil {
		return
	}

	// Find the movie in the session
	var movie *trakt.TrendingMovie
	for i := range session.movies {
		if session.movies[i].Movie.IDs.Trakt == payload.TraktMovieID {
			movie = &session.movies[i]
			break
		}
	}
	if movie == nil {
		return
	}

	w.postMovieToGroupChat(task, session.chatID, *movie, user)

	// Advance to next card
	session.index++
	w.sendNextTrendingCard(task, payload.TelegramID, session, payload.MessageID)
}

// postMovieToGroupChat creates a MovieNotification and sends the formatted card.
func (w *Worker) postMovieToGroupChat(task Task, chatID int64, tm trakt.TrendingMovie, user *storage.User) {
	// Check if already posted to this chat
	exists, err := w.store.HasMovieNotification(task.Ctx, chatID, tm.Movie.IDs.Trakt)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to check movie notification", "error", err)
		return
	}
	if exists {
		return
	}

	// Build thumbnail URL - Trakt returns paths without the protocol prefix
	var photoURL string
	if len(tm.Movie.Images.Thumb) > 0 {
		photoURL = "https://" + tm.Movie.Images.Thumb[0]
	}

	// Fetch top actors for the group chat card
	var actorsStr string
	cast, err := w.trakt.GetMoviePeople(task.Ctx, tm.Movie.IDs.Slug)
	if err != nil {
		slog.ErrorContext(task.Ctx, "failed to fetch movie cast", "error", err, "movie", tm.Movie.Title)
		// Non-fatal — show card without actors
	} else {
		actorsStr = formatTopActors(cast, 4)
	}

	mn := storage.MovieNotification{
		ChatID:       chatID,
		TraktMovieID: tm.Movie.IDs.Trakt,
		MovieTitle:   tm.Movie.Title,
		Year:         tm.Movie.Year,
		Genre:        formatMovieGenres(tm.Movie.Genres),
		Runtime:      tm.Movie.Runtime,
		Rating:       tm.Movie.Rating,
		MovieSlug:    tm.Movie.IDs.Slug,
		IMDBID:       tm.Movie.IDs.IMDB,
		PhotoURL:     photoURL,
		Overview:     tm.Movie.Overview,
		Actors:       actorsStr,
	}

	if err := w.store.CreateMovieNotification(task.Ctx, &mn); err != nil {
		slog.ErrorContext(task.Ctx, "failed to create movie notification", "error", err)
		return
	}

	// Create watch statuses and format the message
	watchedLine := w.createAndFormatWatchStatuses(task.Ctx, storage.NotificationMovie, mn.ID, []uint{user.ID})
	msg := formatMovieNotification(&mn)
	if watchedLine != "" {
		msg += "\n\n" + watchedLine
	}

	w.results <- Result{
		Ctx:           task.Ctx,
		ChatID:        chatID,
		Text:          msg,
		PhotoURL:      mn.PhotoURL,
		InlineButtons: watchButtons(storage.NotificationMovie, mn.ID),
		OnSent: func(messageID int) error {
			return w.store.UpdateMovieNotificationMessageID(task.Ctx, mn.ID, messageID)
		},
	}
}

// handleSkipMovie saves the movie as seen and advances the browse session.
func (w *Worker) handleSkipMovie(task Task) {
	payload, ok := task.Payload.(MovieActionPayload)
	if !ok {
		slog.ErrorContext(task.Ctx, "invalid payload for skip movie task")
		return
	}

	user, err := w.store.GetUserByTelegramID(task.Ctx, payload.TelegramID)
	if err != nil || user == nil {
		slog.ErrorContext(task.Ctx, "failed to look up user for skip", "error", err)
		return
	}

	// Save as followed so it won't appear next week
	if err := w.store.CreateFollowedMovie(task.Ctx, &storage.FollowedMovie{
		UserID:       user.ID,
		TraktMovieID: payload.TraktMovieID,
	}); err != nil {
		slog.ErrorContext(task.Ctx, "failed to save skipped movie", "error", err)
	}

	w.answerCallback(task.Ctx, payload.CallbackQueryID, "Skipped!", false)

	session := w.getMovieSession(payload.TelegramID)
	if session == nil {
		return
	}

	session.index++
	w.sendNextTrendingCard(task, payload.TelegramID, session, payload.MessageID)
}

// handleMoviePrev navigates to the previous movie card without side effects.
func (w *Worker) handleMoviePrev(task Task) {
	payload, ok := task.Payload.(MovieActionPayload)
	if !ok {
		slog.ErrorContext(task.Ctx, "invalid payload for movie prev task")
		return
	}

	w.answerCallback(task.Ctx, payload.CallbackQueryID, "", false)

	session := w.getMovieSession(payload.TelegramID)
	if session == nil {
		return
	}

	if session.index > 0 {
		session.index--
	}
	w.sendNextTrendingCard(task, payload.TelegramID, session, payload.MessageID)
}

// handleMovieNext navigates to the next movie card without side effects.
func (w *Worker) handleMovieNext(task Task) {
	payload, ok := task.Payload.(MovieActionPayload)
	if !ok {
		slog.ErrorContext(task.Ctx, "invalid payload for movie next task")
		return
	}

	w.answerCallback(task.Ctx, payload.CallbackQueryID, "", false)

	session := w.getMovieSession(payload.TelegramID)
	if session == nil {
		return
	}

	if session.index < len(session.movies)-1 {
		session.index++
	}
	w.sendNextTrendingCard(task, payload.TelegramID, session, payload.MessageID)
}
