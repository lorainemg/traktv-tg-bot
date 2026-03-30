package worker

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// tokenRefreshBuffer is how far in advance of expiry we refresh the token.
// Refreshing early avoids a race where the token expires mid-request.
const tokenRefreshBuffer = 24 * time.Hour

// ensureFreshToken checks if a user's Trakt access token is expired or about
// to expire, and refreshes it if needed. Takes a *storage.User (pointer) so
// changes to the token fields are visible to the caller — like passing an
// object by reference in C# or mutating a dict in Python.
// Returns an error if the refresh fails — the caller should skip this user.
func (w *Worker) ensureFreshToken(user *storage.User) error {
	// Token is still fresh — no refresh needed
	if !user.TraktTokenExpiresAt.IsZero() && time.Now().Before(user.TraktTokenExpiresAt.Add(-tokenRefreshBuffer)) {
		return nil
	}

	slog.Info("refreshing trakt token", "user_id", user.ID, "telegram_id", user.TelegramID)

	token, err := w.trakt.RefreshToken(user.TraktRefreshToken)
	if err != nil {
		return fmt.Errorf("refreshing token for user %d: %w", user.TelegramID, err)
	}

	expiresAt := time.Unix(int64(token.CreatedAt+token.ExpiresIn), 0)

	if err := w.store.UpdateUserTokens(user.TelegramID, token.AccessToken, token.RefreshToken, expiresAt); err != nil {
		return fmt.Errorf("saving refreshed token for user %d: %w", user.TelegramID, err)
	}

	// Update the user struct in-place so the caller uses the fresh token
	user.TraktAccessToken = token.AccessToken
	user.TraktRefreshToken = token.RefreshToken
	user.TraktTokenExpiresAt = expiresAt

	slog.Info("trakt token refreshed", "user_id", user.ID, "expires_at", expiresAt)
	return nil
}
