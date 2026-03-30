package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/tmdb"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// pendingInput tracks that the worker is waiting for a text reply in a chat.
// For example, after clicking "Change Country", we expect the next message
// to be a country code.
type pendingInput struct {
	action    string // what we're waiting for: "country", "timezone"
	messageID int    // the config message to edit once the input is received
}

// Worker reads tasks from a channel, processes them using the Trakt API
// and storage service, and sends results back through another channel.
type Worker struct {
	tasks   chan Task       // input queue - other packages send tasks here
	results chan Result     // output queue - worker sends messages to deliver here
	store   storage.Service // database operations (the interface, not the concrete type)
	trakt   *trakt.Client   // Trakt API client
	tmdb    *tmdb.Client    // TMDB API client - used for watch provider lookups

	// pendingInputs tracks chats where the worker expects text input.
	// Accessed from both the bot goroutine (HasPendingInput) and the worker
	// goroutine (setPendingInput, consumePendingInput), so it's protected
	// by a sync.Mutex - Go's mutual exclusion lock, like threading.Lock()
	// in Python or the lock keyword in C#.
	mu            sync.Mutex
	pendingInputs map[int64]pendingInput // chatID → what we're waiting for
}

// New creates a Worker with buffered channels of the given size.
// bufferSize controls how many tasks/results can queue up before
// the sender blocks - like queue.Queue(maxsize=N) in Python.
func New(store storage.Service, traktClient *trakt.Client, tmdbClient *tmdb.Client, bufferSize int) *Worker {
	return &Worker{
		tasks:         make(chan Task, bufferSize),
		results:       make(chan Result, bufferSize),
		store:         store,
		trakt:         traktClient,
		tmdb:          tmdbClient,
		pendingInputs: make(map[int64]pendingInput),
	}
}

// Submit sends a task into the worker's input queue.
// This is safe to call from any goroutine - channels are concurrency-safe by design.
func (w *Worker) Submit(task Task) {
	w.tasks <- task
}

// Results returns a receive-only channel for consuming worker output.
// The "<-chan" type means callers can only READ from this channel, not write to it.
// This is a compile-time safety measure - like exposing a ReadOnlyCollection in C#.
func (w *Worker) Results() <-chan Result {
	return w.results
}

// Run starts the worker's main loop. It blocks until ctx is cancelled.
// Typically launched with "go worker.Run(ctx)" so it runs in the background.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case task := <-w.tasks:
			// A task arrived - dispatch it to the right handler.
			w.process(task)
		case <-ctx.Done():
			// Shutdown signal received - exit the loop cleanly.
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
	case TaskShowConfig:
		w.handleShowConfig(task)
	case TaskToggleDeleteWatched:
		w.handleToggleDeleteWatched(task)
	case TaskTextInput:
		w.handleTextInput(task)
	case TaskPromptCountry:
		w.handlePromptCountry(task)
	case TaskShowTimezones:
		w.handleShowTimezones(task)
	case TaskSetTimezone:
		w.handleSetTimezone(task)
	case TaskUnseen:
		w.handleUnseen(task)
	default:
		slog.Warn("unknown task type", "type", task.Type)
	}
}

// HasPendingInput checks if a chat has a pending text input request.
// Called from the bot goroutine, so it locks the mutex for safe access.
func (w *Worker) HasPendingInput(chatID int64) bool {
	w.mu.Lock()
	// defer ensures Unlock runs when the function returns, even on early returns.
	// Like Python's "with lock:" or C#'s "lock(mu) { ... }" - guarantees release.
	defer w.mu.Unlock()
	_, exists := w.pendingInputs[chatID]
	return exists
}

// setPendingInput records that the worker expects text input from a chat.
// Called from worker handlers (same goroutine as process), but locked
// because HasPendingInput reads from a different goroutine.
func (w *Worker) setPendingInput(chatID int64, input pendingInput) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pendingInputs[chatID] = input
}

// consumePendingInput retrieves and removes a pending input for a chat.
// Returns the input and true if one existed, or zero-value and false if not.
// This is the ", ok" pattern you've seen with type assertions and map lookups.
func (w *Worker) consumePendingInput(chatID int64) (pendingInput, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	input, exists := w.pendingInputs[chatID]
	if exists {
		delete(w.pendingInputs, chatID) // built-in function to remove a map entry
	}
	return input, exists
}
