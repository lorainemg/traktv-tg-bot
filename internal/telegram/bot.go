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
		// By default, Telegram only sends message updates. We need to explicitly
		// request message_reaction updates so the bot receives emoji reactions.
		bot.WithAllowedUpdates(bot.AllowedUpdates{
			"message",
			"message_reaction",
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

// SendResultsMessage sends a message or photo to a Telegram chat based on the provided result and invokes the OnSent callback.
func (b *Bot) SendResultsMessage(result worker.Result) {
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
	msg, err := b.bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID:             result.ChatID,
		MessageThreadID:    result.ThreadID, // 0 sends to General/default topic
		Text:               result.Text,
		ParseMode:          models.ParseModeMarkdownV1,
		LinkPreviewOptions: preview,
	})
	if err != nil {
		fmt.Println("Error sending message:", err)
		return
	}

	// Call the OnSent callback to save the Telegram message ID back to the DB
	if result.OnSent != nil {
		if err := result.OnSent(msg.ID); err != nil {
			fmt.Println("Error in OnSent callback:", err)
		}
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

// handleDefault receives all updates not matched by a specific handler.
// We use it to catch message reactions, since the library has no dedicated
// HandlerType for reactions.
func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	if update.MessageReaction != nil {
		b.handleReaction(update.MessageReaction)
	}
}

// watchedEmoji is the reaction emoji that triggers "mark as watched" on Trakt.
const watchedEmoji = "👀"

// handleReaction processes a message reaction update.
// If a user reacts with 👀 on an episode notification, it submits a task
// to mark that episode as watched on the user's Trakt account.
func (b *Bot) handleReaction(reaction *models.MessageReactionUpdated) {
	// Only process reactions from users (not channels/anonymous)
	if reaction.User == nil {
		return
	}

	// Check if any of the new reactions is the "watched" emoji.
	// NewReaction contains only the reactions that were just added.
	for _, r := range reaction.NewReaction {
		if r.Type == models.ReactionTypeTypeEmoji &&
			r.ReactionTypeEmoji != nil &&
			r.ReactionTypeEmoji.Emoji == watchedEmoji {

			b.worker.Submit(worker.Task{
				Type:   worker.TaskMarkWatched,
				ChatID: reaction.Chat.ID,
				Payload: worker.MarkWatchedPayload{
					TelegramID:        reaction.User.ID,
					ChatID:            reaction.Chat.ID,
					TelegramMessageID: reaction.MessageID,
				},
			})
			return // one reaction is enough, no need to check the rest
		}
	}
}
