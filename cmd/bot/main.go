package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Embeds the IANA timezone database into the binary so time.LoadLocation
	// works even in minimal containers (like distroless) that lack /usr/share/zoneinfo.
	// The blank identifier _ means "import for side effects only" - its init() registers
	// the embedded data with the time package, similar to Python's import-for-side-effects pattern.
	_ "time/tzdata"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	logglobal "go.opentelemetry.io/otel/log/global"

	"github.com/loraine/traktv-tg-bot/internal/otel"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/telegram"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"github.com/loraine/traktv-tg-bot/internal/worker"
)

// requireEnv reads multiple environment variables and exits if any are missing.
// It takes a variadic parameter (...string) - like *args in Python or params string[] in C#.
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
		slog.Error("missing required environment variables", "keys", strings.Join(missing, ", "))
		os.Exit(1)
	}

	return values
}

// startEpisodeChecker submits an episode check task on a schedule.
// Checks immediately on startup, then on the given interval.
func startEpisodeChecker(ctx context.Context, w *worker.Worker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.Submit(worker.Task{Type: worker.TaskCheckEpisodes, Ctx: ctx})

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskCheckEpisodes, Ctx: ctx})
		case <-ctx.Done():
			return
		}
	}
}

// startWatchHistoryChecker detects episodes marked as watched directly on Trakt
// and updates the corresponding TG notification messages.
func startWatchHistoryChecker(ctx context.Context, w *worker.Worker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskCheckWatchHistory, Ctx: ctx})
		case <-ctx.Done():
			return
		}
	}
}

// startDeletionChecker periodically submits a task to process pending message deletions.
func startDeletionChecker(ctx context.Context, w *worker.Worker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskProcessDeletions, Ctx: ctx})
		case <-ctx.Done():
			return
		}
	}
}

// startMovieChecker submits a trending movie check on a weekly schedule.
// In dev mode, runs every 2 minutes for fast feedback.
func startMovieChecker(ctx context.Context, w *worker.Worker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Check immediately on startup
	w.Submit(worker.Task{Type: worker.TaskCheckTrendingMovies, Ctx: ctx})

	for {
		select {
		case <-ticker.C:
			w.Submit(worker.Task{Type: worker.TaskCheckTrendingMovies, Ctx: ctx})
		case <-ctx.Done():
			return
		}
	}
}

func setupLogger() {
	isDev := strings.HasPrefix(strings.ToLower(os.Getenv("ENV")), "dev")
	if isDev {
		level := slog.LevelDebug
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(handler))

	} else {
		level := slog.LevelInfo
		handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(handler))
	}
}

// setupTelemetryLogger routes slog records into OpenTelemetry Logs,
// so Aspire can correlate structured logs with traces.
func setupTelemetryLogger(serviceName string) {
	env := strings.ToLower(os.Getenv("ENV"))
	if env == "" {
		env = "unknown"
	}

	logger := otelslog.NewLogger(serviceName,
		otelslog.WithLoggerProvider(logglobal.GetLoggerProvider()),
		otelslog.WithSource(strings.HasPrefix(env, "dev")),
		otelslog.WithAttributes(
			attribute.String("deployment.environment", env),
		),
	)

	slog.SetDefault(logger)
}

func main() {
	setupLogger()

	env := requireEnv("TELEGRAM_BOT_TOKEN", "DATABASE_URL", "TRAKT_CLIENT_ID", "TRAKT_CLIENT_SECRET", "TMDB_API_KEY")

	// Connect to PostgreSQL - now returns a *PostgresStore that satisfies storage.Service
	store, err := storage.Connect(env["DATABASE_URL"])
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	traktClient := trakt.NewClient(env["TRAKT_CLIENT_ID"], env["TRAKT_CLIENT_SECRET"])
	tmdbClient := tmdb.NewClient(env["TMDB_API_KEY"])

	// Create the worker with a buffer size of 10.
	// The worker orchestrates all background work: episode checks, user linking, etc.
	w := worker.New(store, traktClient, tmdbClient, 10)

	// Create the Telegram bot - it only depends on the worker, nothing else.
	tgBot, err := telegram.NewBot(env["TELEGRAM_BOT_TOKEN"], w)
	if err != nil {
		slog.Error("failed to create telegram bot", "error", err)
		os.Exit(1)
	}

	// signal.NotifyContext creates a context that gets cancelled when the
	// process receives SIGINT (Ctrl+C) or SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := otel.Setup(ctx, "bot")
	if err != nil {
		slog.Error("failed to initialize OpenTelemetry", "error", err)
		os.Exit(1)
	}

	setupTelemetryLogger("bot")
	slog.Info("open telemetry traces and logs initialized")

	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			slog.Error("failed to shutdown OpenTelemetry", "error", err)
		}
	}()

	// Start the worker loop in the background.
	// "go" launches it as a goroutine - it runs concurrently, not blocking main().
	go w.Run(ctx)

	// Start the results forwarder - reads from the worker's output channel
	// and delivers messages via Telegram.
	tgBot.StartResultsForwarder(ctx)

	// Pick tick interval based on ENV: dev runs every 30s for fast feedback,
	// production runs every 30min to avoid hammering the Trakt API.
	tickInterval := 30 * time.Minute
	if strings.HasPrefix(strings.ToLower(os.Getenv("ENV")), "dev") {
		tickInterval = 1 * time.Minute
	}
	slog.Info("ticker interval configured", "interval", tickInterval)

	go startEpisodeChecker(ctx, w, tickInterval)
	go startWatchHistoryChecker(ctx, w, tickInterval)
	go startDeletionChecker(ctx, w, tickInterval)

	// Movie checker: weekly in production, every 2 minutes in dev
	movieInterval := 168 * time.Hour // 1 week
	if strings.HasPrefix(strings.ToLower(os.Getenv("ENV")), "dev") {
		movieInterval = 2 * time.Minute
	}
	go startMovieChecker(ctx, w, movieInterval)

	slog.Info("bot is running, press Ctrl+C to stop")
	tgBot.Start(ctx)
}
