package worker

import "fmt"

// handleMarkWatched looks up which episode a user reacted to, then marks it
// as watched on their Trakt account.
func (w *Worker) handleMarkWatched(task Task) {
	payload, ok := task.Payload.(MarkWatchedPayload)
	if !ok {
		fmt.Println("Error: invalid payload for MarkWatched task")
		return
	}

	// Step 1: find the notification record by the Telegram message the user reacted to
	notification, err := w.store.GetNotificationByMessageID(payload.TelegramMessageID)
	if err != nil {
		fmt.Println("Error looking up notification:", err)
		return
	}
	if notification == nil {
		// Reaction was on a message we don't track — silently ignore
		return
	}

	// Step 2: find the user's Trakt token by their Telegram ID
	user, err := w.store.GetUserByTelegramID(payload.TelegramID)
	if err != nil {
		fmt.Println("Error looking up user:", err)
		return
	}
	if user == nil {
		// User hasn't linked their Trakt account — let them know
		w.results <- Result{
			ChatID: payload.ChatID,
			Text:   "You need to link your Trakt account first. Use /auth to get started.",
		}
		return
	}

	// Step 3: mark the episode as watched on Trakt
	err = w.trakt.MarkEpisodeWatched(
		user.TraktAccessToken,
		notification.TraktShowID,
		notification.Season,
		notification.EpisodeNumber,
	)
	if err != nil {
		fmt.Println("Error marking episode as watched:", err)
		w.results <- Result{
			ChatID: payload.ChatID,
			Text:   fmt.Sprintf("Failed to mark %s %s as watched.", notification.ShowTitle, notification.EpisodeKey()),
		}
		return
	}

	w.results <- Result{
		ChatID: payload.ChatID,
		Text: fmt.Sprintf(
			"%s marked *%s* %s as watched ✅",
			user.MentionLink(), notification.ShowTitle, notification.EpisodeKey(),
		),
	}
}