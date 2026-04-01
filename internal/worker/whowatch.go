package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// handleWhoWatches searches for a show by name and checks which users in the
// chat have it in their Trakt watched list. Uses goroutines to fetch each
// user's shows concurrently — much faster than checking one user at a time.
func (w *Worker) handleWhoWatches(task Task) {
	p := task.Payload.(WhoWatchesPayload)

	show, err := w.searchShow(task.Ctx, p.Query)
	if err != nil {
		slog.Error("whowatch: failed to search shows", "query", p.Query, "error", err)
		return
	}
	if show == nil {
		w.results <- task.TextResult(fmt.Sprintf("No shows found for \"%s\".", p.Query))
		return
	}

	users, err := w.store.GetUsersByChatID(task.Ctx, task.ChatID)
	if err != nil {
		slog.Error("whowatch: failed to get chat users", "chat_id", task.ChatID, "error", err)
		return
	}
	if len(users) == 0 {
		w.results <- task.TextResult("No authenticated users in this chat.")
		return
	}

	watchers := w.findWatchers(task.Ctx, users, show.IDs.Trakt)

	w.results <- task.TextResult(formatWhoWatchesMessage(show.TraktLink(), watchers))
}

// searchShow queries Trakt for the best match and returns nil if nothing is found.
func (w *Worker) searchShow(ctx context.Context, query string) (*trakt.Show, error) {
	results, err := w.trakt.SearchShows(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("searching shows: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	// Return the top result's Show — we asked Trakt for limit=1
	return &results[0].Show, nil
}

// findWatchers checks which users have a show in their Trakt watched list.
// Launches one goroutine per user for concurrent fetching, and collects
// matches into a shared slice protected by a mutex.
func (w *Worker) findWatchers(ctx context.Context, users []storage.User, traktShowID int) []*storage.User {
	watchers := make([]*storage.User, 0, len(users))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := range users {
		user := &users[i]
		wg.Add(1)
		go func(user *storage.User) {
			defer wg.Done()

			rep, err := w.trakt.GetWatchedShows(ctx, w.tokenFor(ctx, user))
			if err != nil {
				slog.Error("watch history: failed to fetch watched shows", "user_id", user.ID, "error", err)
				return
			}
			mu.Lock()
			for _, entry := range rep {
				if entry.Show.IDs.Trakt == traktShowID {
					watchers = append(watchers, user)
					break
				}
			}
			mu.Unlock()
		}(user)
	}
	wg.Wait()
	return watchers
}

// formatWhoWatchesMessage builds the Telegram message showing who watches a show.
func formatWhoWatchesMessage(showLink string, watchers []*storage.User) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📺 *Who watches* %s?\n\n", showLink))

	if len(watchers) == 0 {
		b.WriteString("Nobody in this chat watches this show.")
		return b.String()
	}

	for _, user := range watchers {
		b.WriteString(fmt.Sprintf("▸ %s\n", user.MentionLink()))
	}

	return b.String()
}

