package worker

import "fmt"

// handleSetMuted processes a mute/unmute request for a user.
// It updates the user's muted status in the database and sends
// a confirmation message back through the results channel.
func (w *Worker) handleSetMuted(task Task) {
	// Type assertion: extract MutePayload from task.Payload (which is type "any").
	// This is like casting in C#: (MutePayload)task.Payload — but Go checks at runtime.
	// The ", ok" pattern returns false instead of panicking if the type doesn't match.
	payload, ok := task.Payload.(MutePayload)
	if !ok {
		fmt.Println("Error: invalid payload for TaskSetMuted")
		return
	}

	user, err := w.store.GetUserByTelegramID(payload.TelegramID)
	if err != nil {
		fmt.Println("Error looking up user:", err)
		w.results <- Result{ChatID: payload.ChatID, Text: "Something went wrong, please try again."}
		return
	}
	if user == nil {
		w.results <- Result{ChatID: payload.ChatID, Text: "You need to /auth first before using /mute."}
		return
	}

	err = w.store.UpdateUserMuted(payload.TelegramID, payload.Muted)
	if err != nil {
		w.results <- Result{ChatID: payload.ChatID, Text: fmt.Sprintf("Failed to update user: %v", err)}
		return
	}

	mutedTxt := "resumed"
	if payload.Muted {
		mutedTxt = "muted"
	}
	// Use a Telegram mention link so underscores in names don't break MarkdownV1.
	w.results <- Result{ChatID: payload.ChatID, Text: fmt.Sprintf("Notifications %s for %s", mutedTxt, user.MentionLink())}

}
