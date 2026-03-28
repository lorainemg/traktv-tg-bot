package telegram

import (
	"context"
	"fmt"

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
		},
	})
}

func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	// Ignore non-command messages for now
}
