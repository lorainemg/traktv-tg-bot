package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"gorm.io/gorm"
)

// Bot ties together the Telegram bot, the database, and the Trakt client.
type Bot struct {
	bot         *bot.Bot
	db          *gorm.DB
	traktClient *trakt.Client
}

// NewBot creates and configures a Telegram bot with command handlers.
func NewBot(token string, db *gorm.DB, traktClient *trakt.Client) (*Bot, error) {
	b := &Bot{
		db:          db,
		traktClient: traktClient,
	}

	opts := []bot.Option{
		// bot.WithDefaultHandler registers a fallback for messages that don't match any command.
		bot.WithDefaultHandler(b.handleDefault),
	}

	tgBot, err := bot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	// Register commands
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, b.handleStart)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/link", bot.MatchTypeExact, b.handleLink)

	b.bot = tgBot
	return b, nil
}

// GetBot returns the underlying *bot.Bot for use by other packages (e.g. the poller).
func (b *Bot) GetBot() *bot.Bot {
	return b.bot
}

// Start begins listening for Telegram updates.
// It blocks until the context is cancelled.
func (b *Bot) Start(ctx context.Context) {
	b.bot.Start(ctx)
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

// handleLink starts the Trakt device OAuth flow for the user.
func (b *Bot) handleLink(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	telegramID := update.Message.From.ID

	// Request a new device code from Trakt
	dc, err := b.traktClient.RequestDeviceCode()
	if err != nil {
		fmt.Println("Error requesting device code:", err)
		_, _ = tgBot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Failed to start Trakt linking. Please try again.",
		})
		return
	}

	// Send the user their activation code
	_, _ = tgBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Go to %s and enter code: %s", dc.VerificationURL, dc.UserCode),
	})

	// Poll for the token in a goroutine (runs in the background).
	// "go func() { ... }()" launches a lightweight concurrent function.
	// It captures chatID, telegramID, dc, tgBot, and b from the outer scope (closure).
	go func() {
		// time.NewTicker sends a value on ticker.C every N seconds — like setInterval in JS.
		ticker := time.NewTicker(time.Duration(dc.Interval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			token, err := b.traktClient.PollForToken(dc.DeviceCode)
			if err != nil {
				// Use context.Background() because the original handler ctx may be done by now.
				_, _ = tgBot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID: chatID,
					Text:   fmt.Sprintf("Trakt linking failed: %v", err),
				})
				return
			}
			if token == nil {
				continue
			}

			b.db.Create(&storage.User{
				TelegramID:        telegramID,
				TraktAccessToken:  token.AccessToken,
				TraktRefreshToken: token.RefreshToken,
			})
			_, _ = tgBot.SendMessage(context.Background(), &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "Trakt account linked!",
			})
			return
		}
	}()
}

func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	// Ignore non-command messages for now
}
