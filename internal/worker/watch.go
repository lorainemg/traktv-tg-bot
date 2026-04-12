package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// --- Shared validation ---

// watchActionContext holds the resolved data from validateWatchAction,
// so each handler can focus on its specific logic without repeating lookups.
type watchActionContext struct {
	payload           WatchActionPayload
	user              *storage.User
	watchStatus       storage.WatchStatus
	// Only one of these will be set, depending on NotificationType
	notification      *storage.Notification
	movieNotification *storage.MovieNotification
}

// validateWatchAction runs the shared checks for both mark-watched and
// mark-unwatched: parse payload, look up notification, resolve user, fetch
// watch status, and verify the user follows this show/movie.
// Returns nil if any check fails — the callback is already answered.
func (w *Worker) validateWatchAction(task Task, taskName string) *watchActionContext {
	payload, ok := task.Payload.(WatchActionPayload)
	if !ok {
		slog.Error("invalid payload for " + taskName + " task")
		return nil
	}

	user := w.resolveWatchUser(task.Ctx, payload)
	if user == nil {
		return nil
	}

	wac := &watchActionContext{
		payload: payload,
		user:    user,
	}

	switch payload.NotificationType {
	case storage.NotificationEpisode:
		notification, err := w.store.GetNotificationByID(task.Ctx, payload.NotificationID)
		if err != nil {
			slog.Error("failed to look up notification", "error", err, "notification_id", payload.NotificationID)
			return nil
		}
		if notification == nil {
			return nil
		}
		// Check if the episode has aired yet
		airTime, err := time.Parse(time.RFC3339, notification.FirstAired)
		if err == nil && airTime.After(time.Now()) {
			w.answerCallback(task.Ctx, payload.CallbackQueryID, "This episode hasn't aired yet.", true)
			return nil
		}
		wac.notification = notification

	case storage.NotificationMovie:
		mn, err := w.store.GetMovieNotificationByID(task.Ctx, payload.NotificationID)
		if err != nil {
			slog.Error("failed to look up movie notification", "error", err, "notification_id", payload.NotificationID)
			return nil
		}
		if mn == nil {
			return nil
		}
		wac.movieNotification = mn

	default:
		slog.Error("unknown notification type", "type", payload.NotificationType)
		return nil
	}

	watchStatus, err := w.store.GetUserWatchStatus(task.Ctx, payload.NotificationType, payload.NotificationID, user.ID)
	if err != nil {
		slog.Error("failed to look up watch status", "error", err)
		return nil
	}
	if watchStatus.ID == 0 {
		msg := "You're not following this show."
		if payload.NotificationType == storage.NotificationMovie {
			msg = "You're not following this movie."
		}
		w.answerCallback(task.Ctx, payload.CallbackQueryID, msg, true)
		return nil
	}
	wac.watchStatus = watchStatus

	return wac
}

// watchActionParams holds the variable parts that differ between marking
// watched vs unwatched. Passed to executeWatchAction so one function handles both.
type watchActionParams struct {
	// expectWatched is the current state we require before proceeding.
	expectWatched bool
	// alreadyMsg is the toast shown when already in the desired state.
	alreadyMsg string
	// failMsg is the toast shown when the Trakt API call or DB update fails.
	failMsg string
	// successMsg is the toast shown after a successful action.
	successMsg string
	// syncTraktEpisode calls the Trakt API for episodes.
	syncTraktEpisode func(ctx context.Context, user *storage.User, notification *storage.Notification) error
	// syncTraktMovie calls the Trakt API for movies.
	syncTraktMovie func(ctx context.Context, user *storage.User, mn *storage.MovieNotification) error
	// syncDB updates the local database (mark or unmark).
	syncDB func(ctx context.Context, notificationType storage.NotificationType, notificationID uint, userID uint) error
}

// executeWatchAction is the shared implementation for both watched and unwatched.
// It checks the current state, calls the Trakt API, updates the DB, and refreshes
// the notification message. The params struct controls which direction it goes.
func (w *Worker) executeWatchAction(task Task, taskName string, params watchActionParams) {
	wac := w.validateWatchAction(task, taskName)
	if wac == nil {
		return
	}

	if wac.watchStatus.Watched != params.expectWatched {
		w.answerCallback(task.Ctx, wac.payload.CallbackQueryID, params.alreadyMsg, true)
		return
	}

	// Sync to Trakt based on notification type
	var syncErr error
	switch wac.payload.NotificationType {
	case storage.NotificationEpisode:
		syncErr = params.syncTraktEpisode(task.Ctx, wac.user, wac.notification)
	case storage.NotificationMovie:
		syncErr = params.syncTraktMovie(task.Ctx, wac.user, wac.movieNotification)
	}
	if syncErr != nil {
		slog.Error("trakt sync failed", "action", taskName, "error", syncErr)
		w.answerCallback(task.Ctx, wac.payload.CallbackQueryID, params.failMsg, true)
		return
	}

	if err := params.syncDB(task.Ctx, wac.payload.NotificationType, wac.payload.NotificationID, wac.user.ID); err != nil {
		slog.Error("db sync failed", "action", taskName, "error", err)
		w.answerCallback(task.Ctx, wac.payload.CallbackQueryID, params.failMsg, true)
		return
	}

	w.answerCallback(task.Ctx, wac.payload.CallbackQueryID, params.successMsg, false)

	// Refresh the notification message based on type
	switch wac.payload.NotificationType {
	case storage.NotificationEpisode:
		w.refreshNotificationMessage(task.Ctx, wac.notification, wac.payload.ChatID)
	case storage.NotificationMovie:
		w.refreshMovieNotificationMessage(task.Ctx, wac.movieNotification, wac.payload.ChatID)
	}
}

// handleMarkWatched marks an episode or movie as watched on Trakt and in the DB.
func (w *Worker) handleMarkWatched(task Task) {
	w.executeWatchAction(task, "MarkWatched", watchActionParams{
		expectWatched:    false,
		alreadyMsg:       "You've already watched this.",
		failMsg:          "Failed to mark as watched.",
		successMsg:       "Marked as watched!",
		syncTraktEpisode: w.syncTraktWatched,
		syncTraktMovie:   w.syncTraktMovieWatched,
		syncDB:           w.store.MarkWatchStatus,
	})
}

// handleMarkUnwatched removes a watch from Trakt history and updates the DB.
func (w *Worker) handleMarkUnwatched(task Task) {
	w.executeWatchAction(task, "MarkUnwatched", watchActionParams{
		expectWatched:    true,
		alreadyMsg:       "You haven't watched this yet.",
		failMsg:          "Failed to unmark as watched.",
		successMsg:       "Unmarked as watched!",
		syncTraktEpisode: w.syncTraktUnwatched,
		syncTraktMovie:   w.syncTraktMovieUnwatched,
		syncDB:           w.store.UnmarkWatchStatus,
	})
}

// syncTraktWatched wraps the Trakt client call to match the syncTrakt signature.
func (w *Worker) syncTraktWatched(ctx context.Context, user *storage.User, notification *storage.Notification) error {
	return w.trakt.MarkEpisodeWatched(
		ctx,
		w.tokenFor(ctx, user),
		notification.TraktShowID,
		notification.Season,
		notification.EpisodeNumber,
	)
}

// syncTraktUnwatched wraps the Trakt client call to match the syncTrakt signature.
func (w *Worker) syncTraktUnwatched(ctx context.Context, user *storage.User, notification *storage.Notification) error {
	return w.trakt.UnmarkEpisodeWatched(
		ctx,
		w.tokenFor(ctx, user),
		notification.TraktShowID,
		notification.Season,
		notification.EpisodeNumber,
	)
}

// syncTraktMovieWatched marks a movie as watched on Trakt.
func (w *Worker) syncTraktMovieWatched(ctx context.Context, user *storage.User, mn *storage.MovieNotification) error {
	return w.trakt.MarkMovieWatched(ctx, w.tokenFor(ctx, user), mn.TraktMovieID)
}

// syncTraktMovieUnwatched removes a movie from Trakt watch history.
func (w *Worker) syncTraktMovieUnwatched(ctx context.Context, user *storage.User, mn *storage.MovieNotification) error {
	return w.trakt.UnmarkMovieWatched(ctx, w.tokenFor(ctx, user), mn.TraktMovieID)
}

// --- Shared helpers ---

// resolveWatchUser looks up the reacting user and validates they have a Trakt account.
// Sends an auth prompt if the user hasn't linked their account yet.
func (w *Worker) resolveWatchUser(ctx context.Context, payload WatchActionPayload) *storage.User {
	user, err := w.store.GetUserByTelegramID(ctx, payload.TelegramID)
	if err != nil {
		slog.Error("failed to look up user", "error", err)
		return nil
	}
	if user == nil {
		w.answerCallback(ctx, payload.CallbackQueryID, "You need to link your Trakt account first. Use /sub.", true)
		return nil
	}
	return user
}

// refreshNotificationMessage rebuilds the notification text with the updated
// "Watched by" line and edits the Telegram message. Call this after the DB
// watch status has already been updated (by MarkWatchStatus or UnmarkWatchStatus).
// Respects per-chat config for timezone formatting and deletion behavior.
func (w *Worker) refreshNotificationMessage(ctx context.Context, notification *storage.Notification, chatID int64) {
	settings, err := w.loadChatSettings(ctx, chatID)
	if err != nil {
		slog.Error("failed to load chat settings", "error", err, "chat_id", chatID)
		return
	}

	statuses, err := w.store.GetWatchStatusesByType(ctx, storage.NotificationEpisode, notification.ID)
	if err != nil {
		slog.Error("failed to fetch watch statuses", "error", err)
		return
	}
	haveAllWatched := allWatched(statuses)

	msg := formatNotificationMessage(notification, settings.location)
	if len(statuses) > 0 {
		msg += "\n\n" + formatWatchedByLine(statuses, haveAllWatched)
	}

	// Keep the button only while someone still hasn't watched.
	// nil InlineButtons tells Telegram to remove the existing keyboard.
	var buttons [][]InlineButton
	if !haveAllWatched {
		buttons = watchButtons(storage.NotificationEpisode, notification.ID)
	}

	w.results <- Result{
		Ctx:           ctx,
		ChatID:        chatID,
		Text:          msg,
		PhotoURL:      notification.PhotoURL,
		EditMessageID: notification.TelegramMessageID,
		InlineButtons: buttons,
	}

	// Only schedule deletion if the chat has deleteWatched enabled
	if haveAllWatched && settings.deleteWatched {
		w.scheduleDeletion(ctx, storage.NotificationEpisode, notification.ID, notification.TelegramMessageID, chatID)
	}
}

// refreshMovieNotificationMessage rebuilds the movie notification with updated
// "Watched by" line and edits the Telegram message. Same pattern as refreshNotificationMessage.
func (w *Worker) refreshMovieNotificationMessage(ctx context.Context, mn *storage.MovieNotification, chatID int64) {
	settings, err := w.loadChatSettings(ctx, chatID)
	if err != nil {
		slog.Error("failed to load chat settings", "error", err, "chat_id", chatID)
		return
	}

	statuses, err := w.store.GetWatchStatusesByType(ctx, storage.NotificationMovie, mn.ID)
	if err != nil {
		slog.Error("failed to fetch movie watch statuses", "error", err)
		return
	}
	haveAllWatched := allWatched(statuses)

	msg := formatMovieNotification(mn)
	if len(statuses) > 0 {
		msg += "\n\n" + formatWatchedByLine(statuses, haveAllWatched)
	}

	var buttons [][]InlineButton
	if !haveAllWatched {
		buttons = watchButtons(storage.NotificationMovie, mn.ID)
	}

	w.results <- Result{
		Ctx:           ctx,
		ChatID:        chatID,
		Text:          msg,
		PhotoURL:      mn.PhotoURL,
		EditMessageID: mn.TelegramMessageID,
		InlineButtons: buttons,
	}

	// Schedule deletion if everyone has watched and the chat has deleteWatched enabled
	if haveAllWatched && settings.deleteWatched {
		w.scheduleDeletion(ctx, storage.NotificationMovie, mn.ID, mn.TelegramMessageID, chatID)
	}
}

// answerCallback sends a Result that tells the bot to answer a callback query
// with a toast (showAlert=false) or a modal popup (showAlert=true).
func (w *Worker) answerCallback(ctx context.Context, callbackQueryID, text string, showAlert bool) {
	w.results <- Result{
		Ctx:               ctx,
		CallbackQueryID:   callbackQueryID,
		Text:              text,
		CallbackShowAlert: showAlert,
	}
}

// scheduleDeletion creates a DB record to delete the notification message later.
// The deletion checker ticker will pick it up after the delay has passed.
func (w *Worker) scheduleDeletion(ctx context.Context, notificationType storage.NotificationType, notificationID uint, telegramMessageID int, chatID int64) {
	err := w.store.CreateScheduledDeletion(ctx, &storage.ScheduledDeletion{
		NotificationType: notificationType,
		NotificationID:   notificationID,
		ChatID:           chatID,
		MessageID:        telegramMessageID,
		DeleteAt:         time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		slog.Error("failed to schedule deletion", "error", err)
	}
}
