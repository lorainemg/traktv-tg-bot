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

	// TODO(human): Implement the polling goroutine.
	// Launch a goroutine that polls for the token in the background.
	//
	// Use this structure:
	//   go func() {
	//       ticker := time.NewTicker(time.Duration(dc.Interval) * time.Second)
	//       defer ticker.Stop()
	//
	//       for range ticker.C {
	//           // 1. Call b.traktClient.PollForToken(dc.DeviceCode)
	//           // 2. If err != nil → send error message, return
	//           // 3. If token == nil → continue (user hasn't authorized yet)
	//           // 4. If token != nil → save to DB and send success message, return
	//       }
	//   }()
	//
	// To save to DB, use:
	//   b.db.Create(&storage.User{
	//       TelegramID:        telegramID,
	//       TraktAccessToken:  token.AccessToken,
	//       TraktRefreshToken: token.RefreshToken,
	//   })
	//
	// Notes:
	//   - time.NewTicker creates a channel that sends a value every N seconds
	//   - "for range ticker.C" loops each time the ticker fires (like setInterval in JS)
	//   - The goroutine captures variables from the outer function (closure)
	//   - Use context.Background() for the SendMessage calls inside the goroutine,
	//     since the original ctx may be done by then
	_ = telegramID // remove when implementing
}

func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	// Ignore non-command messages for now
}
