package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// handleSub handles /sub - subscribes a user to episode notifications.
// For new users, it starts the Trakt OAuth device flow.
// For existing users, it re-subscribes (unmutes) them and/or moves
// their notifications to the current chat.
func (w *Worker) handleSub(task Task) {
	payload, ok := task.Payload.(SubPayload)
	if !ok {
		slog.Error("invalid payload for TaskSub")
		return
	}

	existing, err := w.store.GetUserByTelegramID(task.Ctx, payload.TelegramID)
	if err != nil {
		slog.Error("failed to look up existing user", "error", err)
		w.results <- task.TextResult("Something went wrong. Please try again.")
		return
	}

	if existing != nil {
		w.handleExistingUserSub(task, payload, existing)
		// Update names on every /sub - catches Telegram display name changes.
		err = w.store.UpdateUserNames(task.Ctx, payload.TelegramID, payload.FirstName, payload.Username)
		if err != nil {
			slog.Error("failed to update user names", "error", err)
		}
	} else {
		w.handleNewUserSub(task, payload)
		// For new users, names are saved inside pollForToken when the record is created.
	}
}

// handleExistingUserSub handles /sub when the user already has Trakt tokens.
// Covers three scenarios: re-subscribing after /unsub, moving notifications
// to a new chat, or telling the user they're already subscribed.
func (w *Worker) handleExistingUserSub(task Task, payload SubPayload, existing *storage.User) {
	chatMoved := existing.ChatID != task.ChatID

	if chatMoved {
		// Farewell message to the old chat with a clickable user mention.
		w.results <- Result{
			ChatID: existing.ChatID,
			Text:   fmt.Sprintf("%s has moved their notifications to another chat. Their notifications will no longer be sent here.", existing.MentionLink()),
		}
		if err := w.store.UpdateUserChatID(task.Ctx, payload.TelegramID, task.ChatID); err != nil {
			slog.Error("failed to update user chat ID", "error", err)
			w.results <- task.TextResult("Failed to move notifications. Please try again.")
			return
		}
	}

	if existing.Muted {
		if err := w.store.UpdateUserMuted(task.Ctx, payload.TelegramID, false); err != nil {
			slog.Error("failed to unmute user", "error", err)
			w.results <- task.TextResult("Failed to re-subscribe. Please try again.")
			return
		}
		w.results <- task.TextResult(fmt.Sprintf("Welcome back! Notifications resumed for %s", existing.MentionLink()))
		return
	}

	if chatMoved {
		w.results <- task.TextResult("Trakt account already linked! Notifications will now be sent here.")
		return
	}

	w.results <- task.TextResult("You're already subscribed in this chat!")
}

// handleNewUserSub starts the Trakt OAuth device code flow for a first-time user.
func (w *Worker) handleNewUserSub(task Task, payload SubPayload) {
	dc, err := w.trakt.RequestDeviceCode(task.Ctx)
	if err != nil {
		slog.Error("failed to request device code", "error", err)
		w.results <- task.TextResult("Failed to start Trakt auth. Please try again.")
		return
	}

	w.results <- task.TextResult(fmt.Sprintf("Go to %s and enter code: `%s`", dc.VerificationURL, dc.UserCode))

	// Poll in a goroutine so we don't block the worker loop.
	go w.pollForToken(task.Ctx, task.ChatID, task.ThreadID, payload, dc.DeviceCode, dc.Interval, dc.ExpiresIn)
}

// pollForToken repeatedly checks if the user has authorized the device code.
// Runs as a separate goroutine so the worker's main loop stays free.
func (w *Worker) pollForToken(ctx context.Context, chatID int64, threadID int, payload SubPayload, deviceCode string, intervalSecs int, expiresInSecs int) {
	// Build a minimal Task so we can use TextResult inside this goroutine.
	// pollForToken runs outside the worker loop, so it doesn't have the
	// original task - we reconstruct just enough to build Results.
	t := Task{ChatID: chatID, ThreadID: threadID, Ctx: ctx}

	deadline := time.After(time.Duration(expiresInSecs) * time.Second)
	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			w.results <- t.TextResult("Trakt auth timed out. Please try again.")
			return
		case <-ticker.C:
			token, err := w.trakt.PollForToken(ctx, deviceCode)
			if err != nil {
				w.results <- t.TextResult(fmt.Sprintf("Trakt auth failed: %v", err))
				return
			}
			// nil token means "not authorized yet" - keep polling
			if token == nil {
				continue
			}

			// Compute when the token expires: CreatedAt (unix timestamp) + ExpiresIn (seconds)
			expiresAt := time.Unix(int64(token.CreatedAt+token.ExpiresIn), 0)

			// pollForToken only runs for new users, so just create.
			err = w.store.CreateOrUpdateUser(ctx, &storage.User{
				TelegramID:          payload.TelegramID,
				FirstName:           payload.FirstName,
				Username:            payload.Username,
				ChatID:              chatID,
				TraktAccessToken:    token.AccessToken,
				TraktRefreshToken:   token.RefreshToken,
				TraktTokenExpiresAt: expiresAt,
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
}

