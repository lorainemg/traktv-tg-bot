package worker

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// handleStartAuth handles /auth - either moves an existing user's notifications
// to the current chat, or starts the Trakt OAuth device flow for new users.
func (w *Worker) handleStartAuth(task Task) {
	payload, ok := task.Payload.(AuthPayload)
	if !ok {
		slog.Error("invalid payload for StartAuth task")
		return
	}

	existing, err := w.store.GetUserByTelegramID(payload.TelegramID)
	if err != nil {
		slog.Error("failed to look up existing user", "error", err)
		w.results <- task.TextResult("Something went wrong. Please try again.")
		return
	}

	if existing != nil {
		w.handleExistingUserAuth(task, payload, existing)
		// Update names on every /auth - catches Telegram display name changes.
		err = w.store.UpdateUserNames(payload.TelegramID, payload.FirstName, payload.Username)
		if err != nil {
			slog.Error("failed to update user names", "error", err)
		}
	} else {
		w.handleNewUserAuth(task, payload)
		// For new users, names are saved inside pollForToken when the record is created.
	}
}

// handleExistingUserAuth handles /auth when the user already has Trakt tokens.
// If they're in the same chat, it's a no-op. If a different chat, it moves
// their notifications here and sends a farewell to the old chat.
func (w *Worker) handleExistingUserAuth(task Task, payload AuthPayload, existing *storage.User) {
	if existing.ChatID == task.ChatID {
		w.results <- task.TextResult("You are already authenticated in this chat!")
		return
	}

	// Farewell message to the old chat with a clickable user mention.
	w.results <- Result{
		ChatID: existing.ChatID,
		Text:   fmt.Sprintf("%s has moved their notifications to another chat. Their notifications will no longer be sent here.", existing.MentionLink()),
	}

	if err := w.store.UpdateUserChatID(payload.TelegramID, task.ChatID); err != nil {
		slog.Error("failed to update user chat ID", "error", err)
		w.results <- task.TextResult("Failed to move notifications. Please try again.")
		return
	}

	w.results <- task.TextResult("Trakt account already linked! Notifications will now be sent here.")
}

// handleNewUserAuth starts the Trakt OAuth device code flow for a first-time user.
func (w *Worker) handleNewUserAuth(task Task, payload AuthPayload) {
	dc, err := w.trakt.RequestDeviceCode()
	if err != nil {
		slog.Error("failed to request device code", "error", err)
		w.results <- task.TextResult("Failed to start Trakt auth. Please try again.")
		return
	}

	w.results <- task.TextResult(fmt.Sprintf("Go to %s and enter code: `%s`", dc.VerificationURL, dc.UserCode))

	// Poll in a goroutine so we don't block the worker loop.
	go w.pollForToken(task.ChatID, task.ThreadID, payload, dc.DeviceCode, dc.Interval)
}

// pollForToken repeatedly checks if the user has authorized the device code.
// Runs as a separate goroutine so the worker's main loop stays free.
func (w *Worker) pollForToken(chatID int64, threadID int, payload AuthPayload, deviceCode string, intervalSecs int) {
	// Build a minimal Task so we can use TextResult inside this goroutine.
	// pollForToken runs outside the worker loop, so it doesn't have the
	// original task - we reconstruct just enough to build Results.
	t := Task{ChatID: chatID, ThreadID: threadID}

	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		token, err := w.trakt.PollForToken(deviceCode)
		if err != nil {
			w.results <- t.TextResult(fmt.Sprintf("Trakt auth failed: %v", err))
			return
		}
		// nil token means "not authorized yet" - keep polling
		if token == nil {
			continue
		}

		// pollForToken only runs for new users (Case 3), so just create.
		err = w.store.CreateOrUpdateUser(&storage.User{
			TelegramID:        payload.TelegramID,
			FirstName:         payload.FirstName,
			Username:          payload.Username,
			ChatID:            chatID,
			TraktAccessToken:  token.AccessToken,
			TraktRefreshToken: token.RefreshToken,
		})
		if err != nil {
			slog.Error("failed to save user", "error", err)
			w.results <- t.TextResult("Failed to save Trakt account. Please try again.")
			return
		}

		w.results <- t.TextResult("Trakt account linked!")
		return
	}
}
