package worker

import (
	"fmt"
	"log/slog"
)

// handleUnsub processes /unsub - pauses episode notifications for a user.
// The user stays in the database but is marked as muted, so they can
// re-subscribe later with /sub without re-authenticating.
func (w *Worker) handleUnsub(task Task) {
	payload, ok := task.Payload.(UnsubPayload)
	if !ok {
		slog.Error("invalid payload for TaskUnsub")
		return
	}

	user, err := w.store.GetUserByTelegramID(task.Ctx, payload.TelegramID)
	if err != nil {
		slog.Error("failed to look up user", "error", err)
		w.results <- task.TextResult("Something went wrong, please try again.")
		return
	}
	if user == nil {
		w.results <- task.TextResult("You need to /sub first before using /unsub.")
		return
	}

	if err := w.store.UpdateUserMuted(task.Ctx, payload.TelegramID, true); err != nil {
		w.results <- task.TextResult(fmt.Sprintf("Failed to unsubscribe: %v", err))
		return
	}

	w.results <- task.TextResult(fmt.Sprintf("Notifications paused for %s. Use /sub to re-subscribe anytime.", user.MentionLink()))
}

