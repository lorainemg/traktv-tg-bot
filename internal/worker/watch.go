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
	payload      WatchActionPayload
	notification *storage.Notification
	user         *storage.User
	watchStatus  storage.WatchStatus
}

// validateWatchAction runs the shared checks for both mark-watched and
// mark-unwatched: parse payload, look up notification, resolve user, fetch
// watch status, and verify the user follows this show.
// Returns nil if any check fails — the callback is already answered.
func (w *Worker) validateWatchAction(task Task, taskName string) *watchActionContext {
	payload, ok := task.Payload.(WatchActionPayload)
	if !ok {
		slog.Error("invalid payload for " + taskName + " task")
		return nil
	}

	notification, err := w.store.GetNotificationByID(task.Ctx, payload.NotificationID)
	if err != nil {
		slog.Error("failed to look up notification", "error", err, "notification_id", payload.NotificationID)
		return nil
	}
	if notification == nil {
		return nil
	}

	// Check if the episode has aired yet — parse the stored ISO 8601 timestamp
	// and compare against the current UTC time.
	airTime, err := time.Parse(time.RFC3339, notification.FirstAired)
	if err == nil && airTime.After(time.Now()) {
		w.answerCallback(task.Ctx, payload.CallbackQueryID, "This episode hasn't aired yet.", true)
		return nil
	}

	user := w.resolveWatchUser(task.Ctx, payload)
	if user == nil {
		return nil
	}

	watchStatus, err := w.store.GetUserWatchStatus(task.Ctx, notification.ID, user.ID)
	if err != nil {
		slog.Error("failed to look up watch status", "error", err)
		return nil
	}
	if watchStatus.ID == 0 {
		w.answerCallback(task.Ctx, payload.CallbackQueryID, "You're not following this show.", true)
		return nil
	}

	return &watchActionContext{
		payload:      payload,
		notification: notification,
		user:         user,
		watchStatus:  watchStatus,
	}
}

// watchActionParams holds the variable parts that differ between marking
// watched vs unwatched. Passed to executeWatchAction so one function handles both.
type watchActionParams struct {
	// expectWatched is the current state we require before proceeding.
	// true → user must have watched (for unwatching); false → must not have (for watching).
	expectWatched bool
	// alreadyMsg is the toast shown when the episode is already in the desired state.
	alreadyMsg string
	// failMsg is the toast shown when the Trakt API call or DB update fails.
	failMsg string
	// successMsg is the toast shown after a successful action.
	successMsg string
	// syncTrakt is the function that calls the Trakt API (mark or unmark).
	// In Go, functions are first-class values — you can store them in struct
	// fields and pass them around, just like in Python or JavaScript.
	syncTrakt func(ctx context.Context, user *storage.User, notification *storage.Notification) error
	// syncDB is the function that updates the local database (mark or unmark).
	syncDB func(ctx context.Context, notificationID uint, userID uint) error
}

// executeWatchAction is the shared implementation for both watched and unwatched.
// It checks the current state, calls the Trakt API, updates the DB, and refreshes
// the notification message. The params struct controls which direction it goes.
func (w *Worker) executeWatchAction(task Task, taskName string, params watchActionParams) {
	ctx := w.validateWatchAction(task, taskName)
	if ctx == nil {
		return
	}

	if ctx.watchStatus.Watched != params.expectWatched {
		w.answerCallback(task.Ctx, ctx.payload.CallbackQueryID, params.alreadyMsg, true)
		return
	}

	if err := params.syncTrakt(task.Ctx, ctx.user, ctx.notification); err != nil {
		slog.Error("trakt sync failed", "action", taskName, "error", err)
		w.answerCallback(task.Ctx, ctx.payload.CallbackQueryID, params.failMsg, true)
		return
	}

	if err := params.syncDB(task.Ctx, ctx.notification.ID, ctx.user.ID); err != nil {
		slog.Error("db sync failed", "action", taskName, "error", err)
		w.answerCallback(task.Ctx, ctx.payload.CallbackQueryID, params.failMsg, true)
		return
	}

	w.answerCallback(task.Ctx, ctx.payload.CallbackQueryID, params.successMsg, false)
	w.refreshNotificationMessage(task.Ctx, ctx.notification, ctx.payload.ChatID)
}

// handleMarkWatched marks an episode as watched on Trakt and in the DB.
func (w *Worker) handleMarkWatched(task Task) {
	w.executeWatchAction(task, "MarkWatched", watchActionParams{
		expectWatched: false,
		alreadyMsg:    "You've already watched this episode.",
		failMsg:       "Failed to mark as watched.",
		successMsg:    "Marked as watched!",
		syncTrakt:     w.syncTraktWatched,
		syncDB:        w.store.MarkWatchStatus,
	})
}

// handleMarkUnwatched removes an episode from Trakt history and updates the DB.
func (w *Worker) handleMarkUnwatched(task Task) {
	w.executeWatchAction(task, "MarkUnwatched", watchActionParams{
		expectWatched: true,
		alreadyMsg:    "You haven't watched this episode yet.",
		failMsg:       "Failed to unmark as watched.",
		successMsg:    "Unmarked as watched!",
		syncTrakt:     w.syncTraktUnwatched,
		syncDB:        w.store.UnmarkWatchStatus,
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
		w.scheduleDeletion(ctx, notification, chatID)
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
func (w *Worker) scheduleDeletion(ctx context.Context, notification *storage.Notification, chatID int64) {
	err := w.store.CreateScheduledDeletion(ctx, &storage.ScheduledDeletion{
		NotificationID: notification.ID,
		ChatID:         chatID,
		MessageID:      notification.TelegramMessageID,
		DeleteAt:       time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		slog.Error("failed to schedule deletion", "error", err)
	}
}
