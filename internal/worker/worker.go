package worker

import (
	"context"
	"fmt"

	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
)

// TaskType identifies what kind of work a task represents.
// Using a custom type instead of raw int makes the code self-documenting.
type TaskType int

// iota is Go's auto-incrementing constant generator.
// It starts at 0 and increments by 1 for each constant in the block —
// like an auto-numbered enum in C#.
const (
	TaskCheckEpisodes TaskType = iota // = 0
	TaskStartAuth                     // = 1
)

// Task represents a unit of work submitted to the worker queue.
type Task struct {
	Type    TaskType
	ChatID  int64 // where to send Telegram responses
	Payload any   // extra data, varies by task type (like interface{} — accepts any value)
}

// Result represents a message the worker wants delivered via Telegram.
// The worker never talks to Telegram directly — it puts Results on a channel,
// and the Telegram side reads and sends them.
type Result struct {
	ChatID int64
	Text   string
}

// Worker reads tasks from a channel, processes them using the Trakt API
// and storage service, and sends results back through another channel.
type Worker struct {
	tasks   chan Task        // input queue — other packages send tasks here
	results chan Result      // output queue — worker sends messages to deliver here
	store   storage.Service // database operations (the interface, not the concrete type)
	trakt   *trakt.Client   // Trakt API client
}

// New creates a Worker with buffered channels of the given size.
// bufferSize controls how many tasks/results can queue up before
// the sender blocks — like queue.Queue(maxsize=N) in Python.
func New(store storage.Service, traktClient *trakt.Client, bufferSize int) *Worker {
	return &Worker{
		tasks:   make(chan Task, bufferSize),
		results: make(chan Result, bufferSize),
		store:   store,
		trakt:   traktClient,
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
			fmt.Println("Worker stopped.")
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
	default:
		fmt.Printf("Unknown task type: %d\n", task.Type)
	}
}