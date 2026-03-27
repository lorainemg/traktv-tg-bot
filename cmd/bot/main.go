package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/telegram"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// requireEnv reads multiple environment variables and exits if any are missing.
// It takes a variadic parameter (...string) — like *args in Python or params string[] in C#.
// Returns a map so you can look up each value by name.
func requireEnv(keys ...string) map[string]string {
	values := make(map[string]string) // make() creates an initialized map — like dict() in Python
	var missing []string

	for _, key := range keys {
		// range iterates over a slice, returning (index, value) on each step.
		// _ discards the index since we don't need it — like "for _, key in enumerate(keys)" in Python.
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

func main() {
	env := requireEnv("TELEGRAM_BOT_TOKEN", "DATABASE_URL", "TRAKT_CLIENT_ID", "TRAKT_CLIENT_SECRET", "TELEGRAM_CHAT_ID")

	// Connect to PostgreSQL
	db, err := storage.Connect(env["DATABASE_URL"])
	if err != nil {
		fmt.Println("Failed to connect to database:", err)
		os.Exit(1)
	}
	fmt.Println("Connected to database")

	// Create the Trakt API client
	traktClient := trakt.NewClient(env["TRAKT_CLIENT_ID"], env["TRAKT_CLIENT_SECRET"])

	// Create the Telegram bot
	tgBot, err := telegram.NewBot(env["TELEGRAM_BOT_TOKEN"], db, traktClient)
	if err != nil {
		fmt.Println("Failed to create telegram bot:", err)
		os.Exit(1)
	}

	// strconv.ParseInt converts a string to int64.
	// Like int() in Python, but explicit about the base (10 = decimal) and bit size (64).
	chatID, err := strconv.ParseInt(env["TELEGRAM_CHAT_ID"], 10, 64)
	if err != nil {
		fmt.Println("TELEGRAM_CHAT_ID must be a number:", err)
		os.Exit(1)
	}

	// signal.NotifyContext creates a context that gets cancelled when the
	// process receives SIGINT (Ctrl+C) or SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the episode poller (checks every 30 seconds for testing, change to 1*time.Hour for production)
	telegram.StartPoller(ctx, tgBot.GetBot(), db, traktClient, chatID, 30*time.Second)

	fmt.Println("Bot is running... Press Ctrl+C to stop.")
	tgBot.Start(ctx)
}