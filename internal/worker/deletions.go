package worker

import "log/slog"

// handleProcessDeletions checks for notification messages that are due for deletion
// and sends delete instructions to the Telegram bot via the results channel.
func (w *Worker) handleProcessDeletions(task Task) {
	deletions, err := w.store.GetPendingDeletions(task.Ctx)
	if err != nil {
		slog.Error("failed to fetch pending deletions", "error", err)
		return
	}
	for _, deletion := range deletions {
		w.results <- Result{
			Ctx:             task.Ctx,
			ChatID:          deletion.ChatID,
			DeleteMessageID: deletion.MessageID,
		}
		err := w.store.RemoveScheduledDeletion(task.Ctx, deletion.ID)
		if err != nil {
			slog.Error("failed to remove deletion record", "error", err, "deletion_id", deletion.ID)
		}
	}
}
