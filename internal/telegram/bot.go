package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	// models provides Telegram API types like ParseMode, Update, etc.
	"github.com/loraine/traktv-tg-bot/internal/worker"
)

// Bot ties together the Telegram bot and the worker queue.
// The bot is now a pure "UI layer" — it receives commands and forwards them
// to the worker. No database, no Trakt client, no business logic.
type Bot struct {
	bot    *bot.Bot
	worker *worker.Worker
}

// NewBot creates and configures a Telegram bot with command handlers.
func NewBot(token string, w *worker.Worker) (*Bot, error) {
	b := &Bot{
		worker: w,
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(b.handleDefault),
		// By default, Telegram only sends message updates. We need to explicitly
		// request callback_query updates so the bot receives inline button clicks.
		bot.WithAllowedUpdates(bot.AllowedUpdates{
			"message",
			"callback_query",
		}),
	}

	tgBot, err := bot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, b.handleStart)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/auth", bot.MatchTypeExact, b.handleAuth)
	// MatchTypePrefix matches any message starting with "/register_topic" —
	// this lets us capture the argument after the command (e.g. "/register_topic anime").
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/register_topic", bot.MatchTypePrefix, b.handleRegisterTopic)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/upcoming", bot.MatchTypeExact, b.handleUpcoming)
	// MatchTypePrefix so "/mute@BotName" in group chats still matches.
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/mute", bot.MatchTypePrefix, b.handleMute)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/unmute", bot.MatchTypePrefix, b.handleUnmute)

	b.bot = tgBot
	return b, nil
}

// Start begins listening for Telegram updates.
// It blocks until the context is cancelled.
func (b *Bot) Start(ctx context.Context) {
	b.bot.Start(ctx)
}

// SendResultsMessage dispatches a Result to the appropriate Telegram action.
// Priority: CallbackQuery > Delete > Edit > Send new.
func (b *Bot) SendResultsMessage(result worker.Result) {
	switch {
	case result.CallbackQueryID != "":
		b.answerCallbackResult(result)
	case result.DeleteMessageID != 0:
		b.deleteResultsMessage(result)
	case result.EditMessageID != 0:
		b.editResultsMessage(result)
	default:
		b.sendNewMessage(result)
	}
}

// answerCallbackResult answers a callback query with a toast or popup.
func (b *Bot) answerCallbackResult(result worker.Result) {
	_, err := b.bot.AnswerCallbackQuery(context.Background(), &bot.AnswerCallbackQueryParams{
		CallbackQueryID: result.CallbackQueryID,
		Text:            result.Text,
		ShowAlert:       result.CallbackShowAlert,
	})
	if err != nil {
		slog.Error("failed to answer callback query", "error", err)
	}
}

// deleteResultsMessage deletes a Telegram message — used to clean up
// notifications after everyone has watched.
func (b *Bot) deleteResultsMessage(result worker.Result) {
	_, err := b.bot.DeleteMessage(context.Background(), &bot.DeleteMessageParams{
		ChatID:    result.ChatID,
		MessageID: result.DeleteMessageID,
	})
	if err != nil {
		slog.Error("failed to delete message", "error", err, "chat_id", result.ChatID, "message_id", result.DeleteMessageID)
	}
}

// sendNewMessage sends a new Telegram message with optional photo preview and inline buttons.
func (b *Bot) sendNewMessage(result worker.Result) {
	// Build link preview options based on whether we have a photo
	var preview *models.LinkPreviewOptions
	if result.PhotoURL != "" {
		preview = &models.LinkPreviewOptions{
			URL:              &result.PhotoURL,
			PreferLargeMedia: bot.False(), // centered image, smaller than SendPhoto
			ShowAboveText:    bot.True(),  // image above the message text
		}
	} else {
		preview = &models.LinkPreviewOptions{
			IsDisabled: bot.True(),
		}
	}
	params := &bot.SendMessageParams{
		ChatID:             result.ChatID,
		MessageThreadID:    result.ThreadID, // 0 sends to General/default topic
		Text:               result.Text,
		ParseMode:          models.ParseModeMarkdownV1,
		LinkPreviewOptions: preview,
	}
	// Only set ReplyMarkup when we have buttons — a nil *InlineKeyboardMarkup
	// assigned to the ReplyMarkup interface field is non-nil in Go, which causes
	// Telegram to reject the request with "object expected as reply markup".
	if kb := buildInlineKeyboard(result.InlineButtons); kb != nil {
		params.ReplyMarkup = kb
	}
	msg, err := b.bot.SendMessage(context.Background(), params)
	if err != nil {
		slog.Error("failed to send message", "error", err, "chat_id", result.ChatID)
		return
	}

	// Call the OnSent callback to save the Telegram message ID back to the DB
	if result.OnSent != nil {
		if err := result.OnSent(msg.ID); err != nil {
			slog.Error("OnSent callback failed", "error", err, "message_id", msg.ID)
		}
	}
}

// editResultsMessage edits an existing Telegram message with new text.
// Used to update the "Watched by" status line on episode notifications.
func (b *Bot) editResultsMessage(result worker.Result) {
	// Preserve the original thumbnail by passing the same PhotoURL
	var preview *models.LinkPreviewOptions
	if result.PhotoURL != "" {
		preview = &models.LinkPreviewOptions{
			URL:              &result.PhotoURL,
			PreferLargeMedia: bot.False(),
			ShowAboveText:    bot.True(),
		}
	}

	editParams := &bot.EditMessageTextParams{
		ChatID:             result.ChatID,
		MessageID:          result.EditMessageID,
		Text:               result.Text,
		ParseMode:          models.ParseModeMarkdownV1,
		LinkPreviewOptions: preview,
	}
	if kb := buildInlineKeyboard(result.InlineButtons); kb != nil {
		editParams.ReplyMarkup = kb
	}
	_, err := b.bot.EditMessageText(context.Background(), editParams)
	if err != nil {
		// Telegram rejects edits when the content hasn't changed — this is expected
		// when a user clicks the button but was already marked as watched.
		if strings.Contains(err.Error(), "message is not modified") {
			return
		}
		slog.Error("failed to edit message", "error", err, "chat_id", result.ChatID, "message_id", result.EditMessageID)
	}
}

// StartResultsForwarder launches a background goroutine that reads Results
// from the worker's output channel and delivers them as Telegram messages.
func (b *Bot) StartResultsForwarder(ctx context.Context) {
	go func() {
		for {
			select {
			case result := <-b.worker.Results():
				b.SendResultsMessage(result)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// handleStart replies with a welcome message when a user sends /start.
func (b *Bot) handleStart(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	_, err := tgBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Welcome to Traktv-TG-Bot! Use /help for commands.",
	})
	if err != nil {
		slog.Error("failed to send message", "error", err)
	}
}

// handleAuth submits an auth task to the worker, which handles the entire
// Trakt OAuth device flow (request code, poll for token, save user).
func (b *Bot) handleAuth(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.worker.Submit(worker.Task{
		Type:   worker.TaskStartAuth,
		ChatID: update.Message.Chat.ID,
		Payload: worker.AuthPayload{
			TelegramID: update.Message.From.ID,
			ChatID:     update.Message.Chat.ID,
			FirstName:  update.Message.From.FirstName,
			Username:   update.Message.From.Username,
		},
	})
}

// handleRegisterTopic registers the current forum topic for episode routing.
// Usage: /register_topic anime (run inside a forum topic)
func (b *Bot) handleRegisterTopic(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	msg := update.Message

	// Parse the topic name from the command text.
	// strings.TrimPrefix removes the command prefix, leaving just the argument.
	// For "/register_topic anime", this gives us " anime" → trimmed to "anime".
	name := strings.TrimSpace(strings.TrimPrefix(msg.Text, "/register_topic"))
	if name == "" {
		_, _ = tgBot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          msg.Chat.ID,
			MessageThreadID: msg.MessageThreadID,
			Text:            "Usage: /register_topic <name>\nExample: /register_topic anime",
		})
		return
	}

	// MessageThreadID is 0 when the message is NOT in a forum topic.
	// This means the user ran the command in General or a non-forum chat.
	if msg.MessageThreadID == 0 {
		_, _ = tgBot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Please run this command inside a forum topic, not in General.",
		})
		return
	}

	b.worker.Submit(worker.Task{
		Type:   worker.TaskRegisterTopic,
		ChatID: msg.Chat.ID,
		Payload: worker.TopicPayload{
			ChatID:   msg.Chat.ID,
			ThreadID: msg.MessageThreadID,
			Name:     name,
		},
	})
}

// handleUpcoming submits a task to list upcoming episodes for the next 7 days.
func (b *Bot) handleUpcoming(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.worker.Submit(worker.Task{
		Type:   worker.TaskUpcoming,
		ChatID: update.Message.Chat.ID,
	})
}

// handleMute submits a task to stop episode notifications for the calling user.
func (b *Bot) handleMute(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.worker.Submit(worker.Task{
		Type:   worker.TaskSetMuted,
		ChatID: update.Message.Chat.ID,
		Payload: worker.MutePayload{
			TelegramID: update.Message.From.ID,
			ChatID:     update.Message.Chat.ID,
			Muted:      true,
		},
	})
}

// handleUnmute submits a task to resume episode notifications for the calling user.
func (b *Bot) handleUnmute(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.worker.Submit(worker.Task{
		Type:   worker.TaskSetMuted,
		ChatID: update.Message.Chat.ID,
		Payload: worker.MutePayload{
			TelegramID: update.Message.From.ID,
			ChatID:     update.Message.Chat.ID,
			Muted:      false,
		},
	})
}

// buildInlineKeyboard converts our simple InlineButton slices into Telegram's
// InlineKeyboardMarkup type. Returns nil if there are no buttons, which means
// "no keyboard" — Telegram will either not show one (new message) or remove an
// existing one (edit).
func buildInlineKeyboard(buttons [][]worker.InlineButton) *models.InlineKeyboardMarkup {
	if len(buttons) == 0 {
		return nil
	}
	rows := make([][]models.InlineKeyboardButton, len(buttons))
	for i, row := range buttons {
		rows[i] = make([]models.InlineKeyboardButton, len(row))
		for j, btn := range row {
			rows[i][j] = models.InlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.CallbackData,
			}
		}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// handleDefault receives all updates not matched by a specific handler.
// We use it to catch callback queries (inline button clicks), since the
// library has no dedicated HandlerType for them.
func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(ctx, update.CallbackQuery)
	}
}

// handleCallbackQuery processes an inline button click.
// It parses the callback data, submits a task to the worker, and answers
// the callback query so Telegram removes the loading spinner.
func (b *Bot) handleCallbackQuery(ctx context.Context, cq *models.CallbackQuery) {
	if strings.HasPrefix(cq.Data, "watched:") {
		notificationID, err := strconv.ParseUint(strings.TrimPrefix(cq.Data, "watched:"), 10, 64)
		if err != nil {
			return
		}
		b.worker.Submit(worker.Task{
			Type:   worker.TaskMarkWatched,
			ChatID: cq.Message.Message.Chat.ID,
			Payload: worker.MarkWatchedPayload{
				TelegramID:      cq.From.ID,
				ChatID:          cq.Message.Message.Chat.ID,
				NotificationID:  uint(notificationID),
				CallbackQueryID: cq.ID,
			},
		})
	}
}
