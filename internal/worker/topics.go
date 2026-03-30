package worker

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// handleRegisterTopic saves a forum topic mapping so episode notifications
// can be routed to the correct topic based on its name.
func (w *Worker) handleRegisterTopic(task Task) {
	// Type assertion: extract the concrete TopicPayload from the generic `any` field.
	// The comma-ok pattern (value, ok) safely checks the type at runtime —
	// if Payload isn't a TopicPayload, ok is false instead of panicking.
	payload, ok := task.Payload.(TopicPayload)
	if !ok {
		slog.Error("invalid payload for TaskRegisterTopic")
		return
	}

	// Guard: only allow topic registration in chats where at least
	// one user has authenticated — prevents random groups from using the bot.
	registered, err := w.store.HasUserInChat(payload.ChatID)
	if err != nil {
		slog.Error("failed to check chat registration", "error", err)
		return
	}
	if !registered {
		w.results <- Result{
			ChatID:   payload.ChatID,
			ThreadID: payload.ThreadID,
			Text:     "No authenticated users in this chat. Use /auth first.",
		}
		return
	}

	topic := &storage.Topic{
		ChatID:   payload.ChatID,
		ThreadID: payload.ThreadID,
		Name:     strings.ToLower(payload.Name),
	}

	if err := w.store.CreateOrUpdateTopic(topic); err != nil {
		slog.Error("failed to register topic", "error", err)
		w.results <- Result{
			ChatID:   payload.ChatID,
			ThreadID: payload.ThreadID,
			Text:     "Failed to register topic. Please try again.",
		}
		return
	}

	w.results <- Result{
		ChatID:   payload.ChatID,
		ThreadID: payload.ThreadID,
		Text:     fmt.Sprintf("Topic registered as *%s*. Episode notifications matching this category will be sent here.", payload.Name),
	}
}