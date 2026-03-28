package telegram

import (
	"context"
	"fmt"
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

	b.bot = tgBot
	return b, nil
}

// Start begins listening for Telegram updates.
// It blocks until the context is cancelled.
func (b *Bot) Start(ctx context.Context) {
	b.bot.Start(ctx)
}

// StartResultsForwarder launches a background goroutine that reads Results
// from the worker's output channel and delivers them as Telegram messages.
func (b *Bot) StartResultsForwarder(ctx context.Context) {
	go func() {
		for {
			select {
			case result := <-b.worker.Results():
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
				_, _ = b.bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:             result.ChatID,
					MessageThreadID:    result.ThreadID, // 0 sends to General/default topic
					Text:               result.Text,
					ParseMode:          models.ParseModeMarkdownV1,
					LinkPreviewOptions: preview,
				})
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
		fmt.Println("Error sending message:", err)
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

func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	// Ignore non-command messages for now
}
