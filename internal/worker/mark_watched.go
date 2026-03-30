package worker

import (
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// handleMarkWatched looks up which episode a user reacted to, marks it as
// watched on their Trakt account, and edits the original notification message
// to update the "Watched by" status line.
func (w *Worker) handleMarkWatched(task Task) {
	payload, ok := task.Payload.(MarkWatchedPayload)
	if !ok {
		slog.Error("invalid payload for MarkWatched task")
		return
	}

	notification, err := w.store.GetNotificationByID(payload.NotificationID)
	if err != nil {
		slog.Error("failed to look up notification", "error", err, "notification_id", payload.NotificationID)
		return
	}
	if notification == nil {
		return
	}

	user := w.resolveWatchUser(payload)
	if user == nil {
		return
	}
	watchStatus, err := w.store.GetUserWatchStatus(notification.ID, user.ID)
	if err != nil {
		slog.Error("failed to look up watch status", "error", err)
		return
	}
	if watchStatus.ID == 0 {
		w.answerCallback(payload.CallbackQueryID, "You're not following this show.", true)
		return
	}
	if watchStatus.Watched {
		w.answerCallback(payload.CallbackQueryID, "You've already watched this episode.", true)
		return
	}

	if !w.markOnTrakt(user, notification) {
		w.answerCallback(payload.CallbackQueryID, "Failed to mark as watched.", true)
		return
	}

	w.answerCallback(payload.CallbackQueryID, "Marked as watched!", false)
	w.updateNotificationMessage(notification, user.ID, payload.ChatID)
}

// resolveWatchUser looks up the reacting user and validates they have a Trakt account.
// Sends an auth prompt if the user hasn't linked their account yet.
func (w *Worker) resolveWatchUser(payload MarkWatchedPayload) *storage.User {
	user, err := w.store.GetUserByTelegramID(payload.TelegramID)
	if err != nil {
		slog.Error("failed to look up user", "error", err)
		return nil
	}
	if user == nil {
		w.answerCallback(payload.CallbackQueryID, "You need to link your Trakt account first. Use /auth.", true)
		return nil
	}
	return user
}

// markOnTrakt calls the Trakt API to mark an episode as watched.
// Returns false on failure — the caller is responsible for user-facing feedback.
func (w *Worker) markOnTrakt(user *storage.User, notification *storage.Notification) bool {
	err := w.trakt.MarkEpisodeWatched(
		user.TraktAccessToken,
		notification.TraktShowID,
		notification.Season,
		notification.EpisodeNumber,
	)
	if err != nil {
		slog.Error("failed to mark episode as watched", "error", err)
		return false
	}
	return true
}

// updateNotificationMessage marks a user's watch status in the DB, rebuilds the
// notification text with the updated "Watched by" line, and edits the Telegram message.
func (w *Worker) updateNotificationMessage(notification *storage.Notification, userID uint, chatID int64) {
	if err := w.store.MarkWatchStatus(notification.ID, userID); err != nil {
		slog.Error("failed to update watch status", "error", err)
		return
	}

	statuses, err := w.store.GetWatchStatuses(notification.ID)
	if err != nil {
		slog.Error("failed to fetch watch statuses", "error", err)
		return
	}
	//haveAllWatched := allWatched(statuses)
	haveAllWatched := false

	msg := formatNotificationMessage(notification, defaultTimezone)
	if len(statuses) > 0 {
		msg += "\n\n" + formatWatchedByLine(statuses, haveAllWatched)
	}

	// Keep the button only while someone still hasn't watched.
	// nil InlineButtons tells Telegram to remove the existing keyboard.
	var buttons [][]InlineButton
	if !haveAllWatched {
		buttons = watchedButton(notification.ID)
	}

	w.results <- Result{
		ChatID:        chatID,
		Text:          msg,
		PhotoURL:      notification.PhotoURL,
		EditMessageID: notification.TelegramMessageID,
		InlineButtons: buttons,
	}

	// Schedule the notification message for deletion 1 hour from now
	if haveAllWatched {
		w.scheduleDeletion(notification, chatID)
	}
}

// answerCallback sends a Result that tells the bot to answer a callback query
// with a toast (showAlert=false) or a modal popup (showAlert=true).
func (w *Worker) answerCallback(callbackQueryID, text string, showAlert bool) {
	w.results <- Result{
		CallbackQueryID:   callbackQueryID,
		Text:              text,
		CallbackShowAlert: showAlert,
	}
}

// scheduleDeletion creates a DB record to delete the notification message later.
// The deletion checker ticker will pick it up after the delay has passed.
func (w *Worker) scheduleDeletion(notification *storage.Notification, chatID int64) {
	err := w.store.CreateScheduledDeletion(&storage.ScheduledDeletion{
		NotificationID: notification.ID,
		ChatID:         chatID,
		MessageID:      notification.TelegramMessageID,
		DeleteAt:       time.Now().Add(1 * time.Second),
	})
	if err != nil {
		slog.Error("failed to schedule deletion", "error", err)
	}
}
