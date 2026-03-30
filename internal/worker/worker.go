package worker

import (
	"context"
	"log/slog"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// Worker reads tasks from a channel, processes them using the Trakt API
// and storage service, and sends results back through another channel.
type Worker struct {
	tasks   chan Task       // input queue — other packages send tasks here
	results chan Result     // output queue — worker sends messages to deliver here
	store   storage.Service // database operations (the interface, not the concrete type)
	trakt   *trakt.Client   // Trakt API client
	tmdb    *tmdb.Client    // TMDB API client — used for watch provider lookups
}

// New creates a Worker with buffered channels of the given size.
// bufferSize controls how many tasks/results can queue up before
// the sender blocks — like queue.Queue(maxsize=N) in Python.
func New(store storage.Service, traktClient *trakt.Client, tmdbClient *tmdb.Client, bufferSize int) *Worker {
	return &Worker{
		tasks:   make(chan Task, bufferSize),
		results: make(chan Result, bufferSize),
		store:   store,
		trakt:   traktClient,
		tmdb:    tmdbClient,
	}
}

// Submit sends a task into the worker's input queue.
// This is safe to call from any goroutine — channels are concurrency-safe by design.
func (w *Worker) Submit(task Task) {
	w.tasks <- task
}

// Results returns a receive-only channel for consuming worker output.
// The "<-chan" type means callers can only READ from this channel, not write to it.
// This is a compile-time safety measure — like exposing a ReadOnlyCollection in C#.
func (w *Worker) Results() <-chan Result {
	return w.results
}

// Run starts the worker's main loop. It blocks until ctx is cancelled.
// Typically launched with "go worker.Run(ctx)" so it runs in the background.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case task := <-w.tasks:
			// A task arrived — dispatch it to the right handler.
			w.process(task)
		case <-ctx.Done():
			// Shutdown signal received — exit the loop cleanly.
			slog.Info("worker stopped")
			return
		}
	}
}

// process dispatches a task to the appropriate handler based on its type.
func (w *Worker) process(task Task) {
	switch task.Type {
	case TaskCheckEpisodes:
		w.handleCheckEpisodes(task)
	case TaskStartAuth:
		w.handleStartAuth(task)
	case TaskRegisterTopic:
		w.handleRegisterTopic(task)
	case TaskSetMuted:
		w.handleSetMuted(task)
	case TaskMarkWatched:
		w.handleMarkWatched(task)
	case TaskCheckWatchHistory:
		w.handleCheckWatchHistory()
	case TaskProcessDeletions:
		w.handleProcessDeletions()
	case TaskUpcoming:
		w.handleUpcoming(task)
	case TaskShows:
		w.handleShows(task)
	default:
		slog.Warn("unknown task type", "type", task.Type)
	}
}
