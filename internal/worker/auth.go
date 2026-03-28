package worker

import (
	"fmt"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// AuthPayload carries the data needed to start the Trakt OAuth device flow.
type AuthPayload struct {
	TelegramID int64
	ChatID     int64 // the chat where the user ran /auth — notifications go here
}

// handleStartAuth initiates the Trakt device OAuth flow:
// requests a device code, sends the verification URL back through the results channel,
// then polls for the token in a background goroutine.
func (w *Worker) handleStartAuth(task Task) {
	payload, ok := task.Payload.(AuthPayload)
	if !ok {
		fmt.Println("Invalid payload for StartAuth task")
		return
	}

	// Step 1: Request a device code from Trakt
	dc, err := w.trakt.RequestDeviceCode()
	if err != nil {
		fmt.Println("Error requesting device code:", err)
		w.results <- Result{
			ChatID: task.ChatID,
			Text:   "Failed to start Trakt auth. Please try again.",
		}
		return
	}

	// Step 2: Send the verification URL back to the user via results channel
	w.results <- Result{
		ChatID: task.ChatID,
		Text:   fmt.Sprintf("Go to %s and enter code: `%s`", dc.VerificationURL, dc.UserCode),
	}

	// Step 3: Poll for the token in a goroutine so we don't block the worker loop.
	// Without "go", this would block Run() and no other tasks could be processed
	// until the user finishes authorizing (which could take minutes).
	go w.pollForToken(task.ChatID, payload.TelegramID, dc.DeviceCode, dc.Interval)
}

// pollForToken repeatedly checks if the user has authorized the device code.
// Runs as a separate goroutine so the worker's main loop stays free.
func (w *Worker) pollForToken(chatID, telegramID int64, deviceCode string, intervalSecs int) {
	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		token, err := w.trakt.PollForToken(deviceCode)
		if err != nil {
			w.results <- Result{
				ChatID: chatID,
				Text:   fmt.Sprintf("Trakt auth failed: %v", err),
			}
			return
		}
		// nil token means "not authorized yet" — keep polling
		if token == nil {
			continue
		}

		// Token received — save the user with their chat ID
		err = w.store.CreateUser(&storage.User{
			TelegramID:        telegramID,
			ChatID:            chatID,
			TraktAccessToken:  token.AccessToken,
			TraktRefreshToken: token.RefreshToken,
		})
		if err != nil {
			fmt.Println("Error creating user:", err)
			w.results <- Result{
				ChatID: chatID,
				Text:   "Failed to save Trakt account. Please try again.",
			}
			return
		}

		w.results <- Result{
			ChatID: chatID,
			Text:   "Trakt account linked!",
		}
		return
	}
}
