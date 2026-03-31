package worker

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/loraine/traktv-tg-bot/internal/data"
	"github.com/loraine/traktv-tg-bot/internal/storage"
)

// Default chat config values - used when no config row exists in the DB.
const (
	defaultCountry       = "US"
	defaultTimezoneName  = "America/New_York"
	defaultDeleteWatched = true
	defaultNotifyHours   = 12
)

// handleShowConfig fetches the chat's configuration from the database
// and sends a message displaying the current settings with inline buttons
// for changing each one.
func (w *Worker) handleShowConfig(task Task) {
	config, err := w.store.GetChatConfig(task.ChatID)
	if err != nil {
		slog.Error("failed to fetch chat config", "error", err, "chat_id", task.ChatID)
		w.results <- task.TextResult("Something went wrong, please try again.")
		return
	}

	country, timezone, deleteWatched, notifyHours := resolveConfig(config)
	w.results <- buildConfigResult(task.ChatID, task.ThreadID, 0, country, timezone, deleteWatched, notifyHours)
}

// handleToggleDeleteWatched flips the DeleteWatched setting and edits
// the config message in-place to reflect the new state.
func (w *Worker) handleToggleDeleteWatched(task Task) {
	payload, ok := task.Payload.(ConfigCallbackPayload)
	if !ok {
		slog.Error("invalid payload for TaskToggleDeleteWatched")
		return
	}

	config, err := w.store.GetChatConfig(payload.ChatID)
	if err != nil {
		slog.Error("failed to fetch chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	// If no config exists yet, create one with defaults
	country, timezone, deleteWatched, notifyHours := resolveConfig(config)

	// Flip the boolean
	deleteWatched = !deleteWatched

	err = w.store.CreateOrUpdateChatConfig(&storage.ChatConfig{
		ChatID:        payload.ChatID,
		Country:       country,
		Timezone:      timezone,
		DeleteWatched: deleteWatched,
		NotifyHours:   notifyHours,
	})
	if err != nil {
		slog.Error("failed to save chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	// Answer the callback query with a brief toast
	w.results <- Result{
		ChatID:          payload.ChatID,
		CallbackQueryID: payload.CallbackQueryID,
		Text:            fmt.Sprintf("Delete watched: %s", deleteLabel(deleteWatched)),
	}

	// Edit the config message to show updated values
	w.results <- buildConfigResult(payload.ChatID, task.ThreadID, payload.MessageID, country, timezone, deleteWatched, notifyHours)
}

// handlePromptCountry sets up pending input and sends a prompt asking for a country code.
func (w *Worker) handlePromptCountry(task Task) {
	payload, ok := task.Payload.(ConfigCallbackPayload)
	if !ok {
		slog.Error("invalid payload for config:country")
		return
	}

	// Register that we're waiting for text input from this chat
	w.setPendingInput(payload.ChatID, pendingInput{
		action:    "country",
		messageID: payload.MessageID,
	})

	// Answer the callback (removes the loading spinner on the button)
	w.results <- Result{
		ChatID:          payload.ChatID,
		CallbackQueryID: payload.CallbackQueryID,
		Text:            "Send a country code below",
	}

	// Prompt the user for input - ForceReply makes Telegram open the reply UI,
	// which ensures the bot receives the response even in group chats with
	// privacy mode enabled (bots always receive replies to their own messages).
	// Selective limits the ForceReply to the user mentioned in the message text.
	w.results <- Result{
		ChatID:                payload.ChatID,
		ThreadID:              task.ThreadID,
		Text:                  fmt.Sprintf("[​](tg://user?id=%d)Reply with a 2-letter country code (e.g. `US`, `GB`, `BR`)", payload.UserTelegramID),
		ForceReply:            true,
		Selective:             true,
		InputFieldPlaceholder: "e.g. US, GB, BR",
	}
}

// handlePromptNotifyHours sets up pending input and sends a prompt asking
// for the number of hours before air time to send notifications.
func (w *Worker) handlePromptNotifyHours(task Task) {
	payload, ok := task.Payload.(ConfigCallbackPayload)
	if !ok {
		slog.Error("invalid payload for config:notify")
		return
	}

	w.setPendingInput(payload.ChatID, pendingInput{
		action:    "notify",
		messageID: payload.MessageID,
	})

	w.results <- Result{
		ChatID:          payload.ChatID,
		CallbackQueryID: payload.CallbackQueryID,
		Text:            "Send the notify window in hours",
	}

	w.results <- Result{
		ChatID:                payload.ChatID,
		ThreadID:              task.ThreadID,
		Text:                  fmt.Sprintf("[​](tg://user?id=%d)Reply with a number of hours (e.g. `12`, `24`, `48`)", payload.UserTelegramID),
		ForceReply:            true,
		Selective:             true,
		InputFieldPlaceholder: "e.g. 12, 24, 48",
	}
}

// handleShowTimezones looks up timezones for the chat's country and either
// auto-sets the timezone (single-timezone country) or shows inline buttons
// for the user to pick from (multi-timezone country).
func (w *Worker) handleShowTimezones(task Task) {
	payload, ok := task.Payload.(ConfigCallbackPayload)
	if !ok {
		slog.Error("invalid payload for TaskShowTimezones")
		return
	}

	config, err := w.store.GetChatConfig(payload.ChatID)
	if err != nil {
		slog.Error("failed to fetch chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	country, _, deleteWatched, notifyHours := resolveConfig(config)

	timezones := data.GetTimezonesForCountry(country)
	if len(timezones) == 0 {
		w.results <- Result{
			ChatID:            payload.ChatID,
			CallbackQueryID:   payload.CallbackQueryID,
			CallbackShowAlert: true,
			Text:              fmt.Sprintf("No timezone data for country %q. Change your country first.", country),
		}
		return
	}

	// Single timezone - auto-set it without asking
	if len(timezones) == 1 {
		err = w.store.CreateOrUpdateChatConfig(&storage.ChatConfig{
			ChatID:        payload.ChatID,
			Country:       country,
			Timezone:      timezones[0],
			DeleteWatched: deleteWatched,
			NotifyHours:   notifyHours,
		})
		if err != nil {
			slog.Error("failed to save chat config", "error", err, "chat_id", payload.ChatID)
			return
		}

		w.results <- Result{
			ChatID:          payload.ChatID,
			CallbackQueryID: payload.CallbackQueryID,
			Text:            fmt.Sprintf("Timezone set to %s", timezones[0]),
		}
		w.results <- buildConfigResult(payload.ChatID, task.ThreadID, payload.MessageID, country, timezones[0], deleteWatched, notifyHours)
		return
	}

	// Multiple timezones - show inline buttons, one per row
	// (timezone names are long, so one column reads better)
	buttons := make([][]InlineButton, len(timezones))
	for i, tz := range timezones {
		buttons[i] = []InlineButton{
			{Text: tz, CallbackData: "config:tz:" + tz},
		}
	}

	w.results <- Result{
		ChatID:          payload.ChatID,
		CallbackQueryID: payload.CallbackQueryID,
		Text:            "Pick a timezone",
	}

	w.results <- Result{
		ChatID:        payload.ChatID,
		ThreadID:      task.ThreadID,
		EditMessageID: payload.MessageID,
		Text:          fmt.Sprintf("Select a timezone for *%s*:", country),
		InlineButtons: buttons,
	}
}

// handleSetTimezone saves a user-selected timezone from the inline button picker.
func (w *Worker) handleSetTimezone(task Task) {
	payload, ok := task.Payload.(TimezonePayload)
	if !ok {
		slog.Error("invalid payload for TaskSetTimezone")
		return
	}

	config, err := w.store.GetChatConfig(payload.ChatID)
	if err != nil {
		slog.Error("failed to fetch chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	country, _, deleteWatched, notifyHours := resolveConfig(config)

	err = w.store.CreateOrUpdateChatConfig(&storage.ChatConfig{
		ChatID:        payload.ChatID,
		Country:       country,
		Timezone:      payload.Timezone,
		DeleteWatched: deleteWatched,
		NotifyHours:   notifyHours,
	})
	if err != nil {
		slog.Error("failed to save chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	w.results <- Result{
		ChatID:          payload.ChatID,
		CallbackQueryID: payload.CallbackQueryID,
		Text:            fmt.Sprintf("Timezone set to %s", payload.Timezone),
	}

	// Replace the timezone picker with the updated config message
	w.results <- buildConfigResult(payload.ChatID, task.ThreadID, payload.MessageID, country, payload.Timezone, deleteWatched, notifyHours)
}

// handleTextInput processes text messages that were sent in response to a pending prompt.
func (w *Worker) handleTextInput(task Task) {
	payload, ok := task.Payload.(TextInputPayload)
	if !ok {
		slog.Error("invalid payload for TaskTextInput")
		return
	}

	pending, exists := w.consumePendingInput(payload.ChatID)
	if !exists {
		return // no pending input - ignore the message
	}

	switch pending.action {
	case "country":
		w.handleCountryInput(task.ThreadID, payload, pending)
	case "notify":
		w.handleNotifyHoursInput(task.ThreadID, payload, pending)
	}
}

// handleCountryInput validates and saves a country code from user text input.
func (w *Worker) handleCountryInput(threadID int, payload TextInputPayload, pending pendingInput) {
	// Normalize: trim whitespace and uppercase
	country := strings.TrimSpace(strings.ToUpper(payload.Text))

	if len(country) != 2 {
		w.results <- Result{
			ChatID:   payload.ChatID,
			ThreadID: threadID,
			Text:     "That doesn't look like a 2-letter country code. Try again with /config.",
		}
		return
	}

	// Load current config to preserve other settings
	config, err := w.store.GetChatConfig(payload.ChatID)
	if err != nil {
		slog.Error("failed to fetch chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	_, timezone, deleteWatched, notifyHours := resolveConfig(config)

	err = w.store.CreateOrUpdateChatConfig(&storage.ChatConfig{
		ChatID:        payload.ChatID,
		Country:       country,
		Timezone:      timezone,
		DeleteWatched: deleteWatched,
		NotifyHours:   notifyHours,
	})
	if err != nil {
		slog.Error("failed to save chat config", "error", err, "chat_id", payload.ChatID)
		w.results <- Result{ChatID: payload.ChatID, ThreadID: threadID, Text: "Something went wrong, please try again."}
		return
	}

	// Edit the original config message with updated values
	w.results <- buildConfigResult(payload.ChatID, threadID, pending.messageID, country, timezone, deleteWatched, notifyHours)
}

// handleNotifyHoursInput validates and saves a notify window from user text input.
func (w *Worker) handleNotifyHoursInput(threadID int, payload TextInputPayload, pending pendingInput) {
	hours, err := strconv.Atoi(strings.TrimSpace(payload.Text))
	if err != nil || hours < 1 || hours > 24*7 {
		w.results <- Result{ChatID: payload.ChatID, ThreadID: threadID, Text: "Please enter a number between 1 and 168. Try again with /config."}
		return
	}
	// Load current config to preserve other settings
	config, err := w.store.GetChatConfig(payload.ChatID)
	if err != nil {
		slog.Error("failed to fetch chat config", "error", err, "chat_id", payload.ChatID)
		return
	}

	country, timezone, deleteWatched, _ := resolveConfig(config)

	err = w.store.CreateOrUpdateChatConfig(&storage.ChatConfig{
		ChatID:        payload.ChatID,
		Country:       country,
		Timezone:      timezone,
		DeleteWatched: deleteWatched,
		NotifyHours:   hours,
	})
	if err != nil {
		slog.Error("failed to save chat config", "error", err, "chat_id", payload.ChatID)
		w.results <- Result{ChatID: payload.ChatID, ThreadID: threadID, Text: "Something went wrong, please try again."}
		return
	}

	w.results <- buildConfigResult(payload.ChatID, threadID, pending.messageID, country, timezone, deleteWatched, hours)
}

// chatSettings holds the resolved config values for a chat, including a parsed
// *time.Location ready to pass to formatting functions.
type chatSettings struct {
	country       string
	timezone      string
	location      *time.Location
	deleteWatched bool
	notifyHours   int
}

// loadChatSettings fetches the config for a chat and resolves all values,
// including parsing the timezone into a *time.Location. Falls back to defaults
// for any unset fields. Returns an error only on DB or timezone parse failures.
func (w *Worker) loadChatSettings(chatID int64) (chatSettings, error) {
	config, err := w.store.GetChatConfig(chatID)
	if err != nil {
		return chatSettings{}, fmt.Errorf("fetching chat config for chat %d: %w", chatID, err)
	}

	country, tzName, deleteWatched, notifyHours := resolveConfig(config)

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Invalid timezone in DB - fall back to default rather than breaking
		slog.Warn("invalid timezone in config, using default", "timezone", tzName, "chat_id", chatID)
		loc = defaultTimezone
	}

	return chatSettings{
		country:       country,
		timezone:      tzName,
		location:      loc,
		deleteWatched: deleteWatched,
		notifyHours:   notifyHours,
	}, nil
}

// resolveConfig extracts values from a ChatConfig, falling back to defaults
// for any unset fields or when config is nil.
func resolveConfig(config *storage.ChatConfig) (country, timezone string, deleteWatched bool, notifyHours int) {
	country = defaultCountry
	timezone = defaultTimezoneName
	deleteWatched = defaultDeleteWatched
	notifyHours = defaultNotifyHours

	if config != nil {
		if config.Country != "" {
			country = config.Country
		}
		if config.Timezone != "" {
			timezone = config.Timezone
		}
		deleteWatched = config.DeleteWatched
		// GORM sets new int columns to 0 for existing rows, so treat 0 as "not set"
		if config.NotifyHours > 0 {
			notifyHours = config.NotifyHours
		}
	}
	return
}

// buildConfigResult constructs a Result with the formatted settings message
// and inline buttons. When editMessageID is non-zero, the result edits an
// existing message instead of sending a new one.
func buildConfigResult(chatID int64, threadID int, editMessageID int, country, timezone string, deleteWatched bool, notifyHours int) Result {
	text := fmt.Sprintf(
		"⚙️ *Chat Settings*\n\n"+
			"🌍 Country: *%s*\n"+
			"_Used for streaming provider lookups_\n\n"+
			"🕐 Timezone: *%s*\n"+
			"_Controls the time shown in notifications_\n\n"+
			"🗑 Delete watched: *%s*\n"+
			"_Auto-delete messages once everyone has watched_\n\n"+
			"🔔 Notify window: *%dh*\n"+
			"_Only notify about episodes airing within this many hours_",
		country, timezone, deleteLabel(deleteWatched), notifyHours,
	)

	return Result{
		ChatID:        chatID,
		ThreadID:      threadID,
		EditMessageID: editMessageID,
		Text:          text,
		InlineButtons: [][]InlineButton{
			{
				{Text: "Change Country", CallbackData: "config:country"},
				{Text: "Change Timezone", CallbackData: "config:timezone"},
			},
			{
				{Text: "Toggle Delete Watched", CallbackData: "config:delete"},
			},
			{
				{Text: "Change Notify Window", CallbackData: "config:notify"},
			},
		},
	}
}

// deleteLabel returns a human-readable label for the delete-watched toggle.
func deleteLabel(on bool) string {
	if on {
		return "On"
	}
	return "Off"
}
