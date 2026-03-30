package worker

import "log/slog"

// handleProcessDeletions checks for notification messages that are due for deletion
// and sends delete instructions to the Telegram bot via the results channel.
func (w *Worker) handleProcessDeletions() {
	deletions, err := w.store.GetPendingDeletions()
	if err != nil {
		slog.Error("failed to fetch pending deletions", "error", err)
		return
	}
	for _, deletion := range deletions {
		w.results <- Result{
			ChatID:          deletion.ChatID,
			DeleteMessageID: deletion.MessageID,
		}
		err := w.store.RemoveScheduledDeletion(deletion.ID)
		if err != nil {
			slog.Error("failed to remove deletion record", "error", err, "deletion_id", deletion.ID)
		}
	}
}
