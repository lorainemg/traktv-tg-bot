package worker

import (
	"fmt"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// handleMarkWatched looks up which episode a user reacted to, marks it as
// watched on their Trakt account, and edits the original notification message
// to update the "Watched by" status line.
func (w *Worker) handleMarkWatched(task Task) {
	payload, ok := task.Payload.(MarkWatchedPayload)
	if !ok {
		fmt.Println("Error: invalid payload for MarkWatched task")
		return
	}

	notification, err := w.store.GetNotificationByID(payload.NotificationID)
	if err != nil {
		fmt.Println("Error looking up notification:", err)
		return
	}
	if notification == nil {
		return
	}

	user := w.resolveWatchUser(payload)
	if user == nil {
		return
	}

	if !w.markOnTrakt(user, notification, payload.ChatID) {
		return
	}

	w.updateNotificationMessage(notification, user.ID, payload.ChatID)
}

// resolveWatchUser looks up the reacting user and validates they have a Trakt account.
// Sends an auth prompt if the user hasn't linked their account yet.
func (w *Worker) resolveWatchUser(payload MarkWatchedPayload) *storage.User {
	user, err := w.store.GetUserByTelegramID(payload.TelegramID)
	if err != nil {
		fmt.Println("Error looking up user:", err)
		return nil
	}
	if user == nil {
		w.results <- Result{
			ChatID: payload.ChatID,
			Text:   "You need to link your Trakt account first. Use /auth to get started.",
		}
		return nil
	}
	return user
}

// markOnTrakt calls the Trakt API to mark an episode as watched.
// Returns false on failure and sends an error message to the chat.
func (w *Worker) markOnTrakt(user *storage.User, notification *storage.Notification, chatID int64) bool {
	err := w.trakt.MarkEpisodeWatched(
		user.TraktAccessToken,
		notification.TraktShowID,
		notification.Season,
		notification.EpisodeNumber,
	)
	if err != nil {
		fmt.Println("Error marking episode as watched:", err)
		w.results <- Result{
			ChatID: chatID,
			Text:   fmt.Sprintf("Failed to mark %s %s as watched.", notification.ShowTitle, notification.EpisodeKey()),
		}
		return false
	}
	return true
}

// updateNotificationMessage marks a user's watch status in the DB, rebuilds the
// notification text with the updated "Watched by" line, and edits the Telegram message.
func (w *Worker) updateNotificationMessage(notification *storage.Notification, userID uint, chatID int64) {
	if err := w.store.MarkWatchStatus(notification.ID, userID); err != nil {
		fmt.Println("Error updating watch status:", err)
		return
	}

	statuses, err := w.store.GetWatchStatuses(notification.ID)
	if err != nil {
		fmt.Println("Error fetching watch statuses:", err)
		return
	}
	haveAllWatched := allWatched(statuses)

	msg := formatNotificationMessage(notification)
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
		fmt.Println("Error scheduling deletion:", err)
	}
}
