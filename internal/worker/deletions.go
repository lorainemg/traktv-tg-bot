package worker

import "fmt"

// handleProcessDeletions checks for notification messages that are due for deletion
// and sends delete instructions to the Telegram bot via the results channel.
func (w *Worker) handleProcessDeletions() {
	deletions, err := w.store.GetPendingDeletions()
	if err != nil {
		fmt.Println("Error fetching pending deletions:", err)
		return
	}
	for _, deletion := range deletions {
		w.results <- Result{
			ChatID:          deletion.ChatID,
			DeleteMessageID: deletion.MessageID,
		}
		err := w.store.RemoveScheduledDeletion(deletion.ID)
		if err != nil {
			fmt.Println("Error removing deletion record:", err)
		}
	}
}
