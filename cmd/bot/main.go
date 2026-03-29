package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/telegram"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/loraine/traktv-tg-bot/internal/worker"
)

// requireEnv reads multiple environment variables and exits if any are missing.
// It takes a variadic parameter (...string) — like *args in Python or params string[] in C#.
// Returns a map so you can look up each value by name.
func requireEnv(keys ...string) map[string]string {
	values := make(map[string]string)
	var missing []string

	for _, key := range keys {
		val := os.Getenv(key)
		if val == "" {
			missing = append(missing, key)
		} else {
			values[key] = val
		}
	}

	if len(missing) > 0 {
		fmt.Printf("Missing required environment variables: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}

	return values
}

// startEpisodeChecker submits an episode check task on a schedule.
// Checks immediately on startup, then every 30 seconds (change to 1h for production).
func startEpisodeChecker(ctx context.Context, w *worker.Worker) {
	ticker := time.NewTicker(30 * time.Minute) // change to 1*time.Hour for production
	defer ticker.Stop()

	w.Submit(worker.Task{Type: worker.TaskCheckEpisodes})

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskCheckEpisodes})
		case <-ctx.Done():
			return
		}
	}
}

// startWatchHistoryChecker detects episodes marked as watched directly on Trakt
// and updates the corresponding TG notification messages.
func startWatchHistoryChecker(ctx context.Context, w *worker.Worker) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskCheckWatchHistory})
		case <-ctx.Done():
			return
		}
	}
}

// startDeletionChecker periodically submits a task to process pending message deletions.
// Runs every 30 minutes — matching the "delete ~1 hour after all watched" requirement
// without needing precise timing.
func startDeletionChecker(ctx context.Context, w *worker.Worker) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskProcessDeletions})
		case <-ctx.Done():
			return
		}
	}
}

func main() {
	env := requireEnv("TELEGRAM_BOT_TOKEN", "DATABASE_URL", "TRAKT_CLIENT_ID", "TRAKT_CLIENT_SECRET", "TMDB_API_KEY")

	// Connect to PostgreSQL — now returns a *PostgresStore that satisfies storage.Service
	store, err := storage.Connect(env["DATABASE_URL"])
	if err != nil {
		fmt.Println("Failed to connect to database:", err)
		os.Exit(1)
	}
	fmt.Println("Connected to database")

	traktClient := trakt.NewClient(env["TRAKT_CLIENT_ID"], env["TRAKT_CLIENT_SECRET"])
	tmdbClient := tmdb.NewClient(env["TMDB_API_KEY"])

	// Create the worker with a buffer size of 10.
	// The worker orchestrates all background work: episode checks, user linking, etc.
	w := worker.New(store, traktClient, tmdbClient, 10)

	// Create the Telegram bot — it only depends on the worker, nothing else.
	tgBot, err := telegram.NewBot(env["TELEGRAM_BOT_TOKEN"], w)
	if err != nil {
		fmt.Println("Failed to create telegram bot:", err)
		os.Exit(1)
	}

	// signal.NotifyContext creates a context that gets cancelled when the
	// process receives SIGINT (Ctrl+C) or SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the worker loop in the background.
	// "go" launches it as a goroutine — it runs concurrently, not blocking main().
	go w.Run(ctx)

	// Start the results forwarder — reads from the worker's output channel
	// and delivers messages via Telegram.
	tgBot.StartResultsForwarder(ctx)

	go startEpisodeChecker(ctx, w)
	go startWatchHistoryChecker(ctx, w)
	go startDeletionChecker(ctx, w)

	fmt.Println("Bot is running... Press Ctrl+C to stop.")
	tgBot.Start(ctx)
}
