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
// The bot is now a pure "UI layer" - it receives commands and forwards them
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

	// MatchTypePrefix so "/cmd@BotName" in group chats still matches.
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypePrefix, b.handleHelp)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypePrefix, b.handleStart)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/auth", bot.MatchTypePrefix, b.handleAuth)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/register_topic", bot.MatchTypePrefix, b.handleRegisterTopic)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/upcoming", bot.MatchTypePrefix, b.handleUpcoming)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/shows", bot.MatchTypePrefix, b.handleShows)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/mute", bot.MatchTypePrefix, b.handleMute)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/unmute", bot.MatchTypePrefix, b.handleUnmute)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/config", bot.MatchTypePrefix, b.handleConfig)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/unseen", bot.MatchTypePrefix, b.handleUnseen)

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

// deleteResultsMessage deletes a Telegram message - used to clean up
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
	// Set ReplyMarkup: ForceReply takes priority (prompts user to reply),
	// otherwise use inline keyboard buttons if present.
	// A nil pointer assigned to the ReplyMarkup interface field is non-nil in Go,
	// which causes Telegram to reject the request - so we only set it when needed.
	if result.ForceReply {
		params.ReplyMarkup = &models.ForceReply{
			ForceReply:            true,
			InputFieldPlaceholder: result.InputFieldPlaceholder,
		}
	} else if kb := buildInlineKeyboard(result.InlineButtons); kb != nil {
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
		// Telegram rejects edits when the content hasn't changed - this is expected
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

// handleHelp replies with a friendly overview of all available commands.
func (b *Bot) handleHelp(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	helpText := `Hey there! Here's what I can do:

<b>What happens automatically</b>
• When a new episode airs for a show anyone here follows, I post a notification with details and streaming links
• Each notification tracks who's watched - click "✅ Mark as Watched" to update your status (this also syncs to your Trakt account)
• If you watch on Trakt directly, I'll pick that up too
• Notifications can auto-delete once everyone's watched (toggle via /config)

<b>Commands</b>
/auth - Link your <a href="https://trakt.tv">Trakt.tv</a> account so I can track your shows
/upcoming [days] - See what's airing soon (default: 7 days, max: 31)
/unseen [@user] - See unseen episode counts (yours, or reply/mention someone)
/shows - See all followed shows and who's watching them
/register_topic &lt;genre&gt; - Route episode notifications of a genre to this group topic
/config - Chat settings: country, timezone, auto-delete watched notifications
/mute - Take a break from episode notifications
/unmute - Turn notifications back on

Just /auth to get started and I'll handle the rest!`

	_, err := tgBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:             update.Message.Chat.ID,
		Text:               helpText,
		ParseMode:          models.ParseModeHTML,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	})
	if err != nil {
		slog.Error("failed to send help message", "error", err)
	}
}

// handleStart replies with a welcome message when a user sends /start.
func (b *Bot) handleStart(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	_, err := tgBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:             update.Message.Chat.ID,
		Text:               "Hey! I'm your TV show companion. I'll keep you posted when new episodes air and track what everyone's watching.\n\nGet started by linking your [Trakt.tv](https://trakt.tv) account with /auth, or check /help to see everything I can do.",
		ParseMode:          models.ParseModeMarkdownV1,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
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
	// In group chats Telegram sends "/register_topic@BotName anime",
	// so we split on the first space to skip the command (with or without @suffix).
	_, name, _ := strings.Cut(msg.Text, " ")
	name = strings.TrimSpace(name)
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

// handleUpcoming parses an optional "days" argument and submits
// a task to list upcoming episodes for that many days (default 7).
func (b *Bot) handleUpcoming(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	_, daysTxt, _ := strings.Cut(update.Message.Text, " ")
	daysTxt = strings.TrimSpace(daysTxt)
	days := 7

	if daysTxt != "" {
		// var declares err without assigning - needed so we can use = (not :=) for days.
		// Using := here would create a NEW days scoped to this if-block, shadowing the outer one.
		var err error
		days, err = strconv.Atoi(daysTxt)
		if err != nil || days < 1 || days > 31 {
			_, _ = tgBot.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:          update.Message.Chat.ID,
				MessageThreadID: update.Message.MessageThreadID,
				Text:            "Usage: /upcoming [days]\nExample: /upcoming 14 or /upcoming",
			})
			return
		}
	}

	b.worker.Submit(worker.Task{
		Type:    worker.TaskUpcoming,
		ChatID:  update.Message.Chat.ID,
		Payload: days,
	})
}

// handleShows submits a task to list all followed shows in this chat.
func (b *Bot) handleShows(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.worker.Submit(worker.Task{
		Type:   worker.TaskShows,
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

// handleConfigCallback routes config inline button clicks to the appropriate task.
func (b *Bot) handleConfigCallback(cq *models.CallbackQuery) {
	action := strings.TrimPrefix(cq.Data, "config:")
	payload := worker.ConfigCallbackPayload{
		ChatID:          cq.Message.Message.Chat.ID,
		CallbackQueryID: cq.ID,
		MessageID:       cq.Message.Message.ID,
	}

	switch {
	case action == "delete":
		b.worker.Submit(worker.Task{
			Type:    worker.TaskToggleDeleteWatched,
			ChatID:  payload.ChatID,
			Payload: payload,
		})
	case action == "country":
		b.worker.Submit(worker.Task{
			Type:    worker.TaskPromptCountry,
			ChatID:  payload.ChatID,
			Payload: payload,
		})
	case action == "timezone":
		b.worker.Submit(worker.Task{
			Type:    worker.TaskShowTimezones,
			ChatID:  payload.ChatID,
			Payload: payload,
		})
	case strings.HasPrefix(action, "tz:"):
		// User picked a specific timezone from the button list.
		// Callback data format: "config:tz:America/New_York"
		b.worker.Submit(worker.Task{
			Type:   worker.TaskSetTimezone,
			ChatID: cq.Message.Message.Chat.ID,
			Payload: worker.TimezonePayload{
				ChatID:          cq.Message.Message.Chat.ID,
				CallbackQueryID: cq.ID,
				MessageID:       cq.Message.Message.ID,
				Timezone:        strings.TrimPrefix(action, "tz:"),
			},
		})
	}
}

// handleUnseen submits a task to list unseen episodes for a user.
// Supports three forms:
//   - /unseen           — your own unseen episodes
//   - /unseen @username — unseen episodes for the mentioned user
//   - reply to a message with /unseen — unseen episodes for that user
func (b *Bot) handleUnseen(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	msg := update.Message
	payload := worker.UnseenPayload{RequesterID: msg.From.ID}

	// Check for @username argument: "/unseen @loraine" or "/unseen loraine"
	_, arg, _ := strings.Cut(msg.Text, " ")
	arg = strings.TrimSpace(arg)
	arg = strings.TrimPrefix(arg, "@") // strip leading @ if present

	if arg != "" {
		payload.TargetUsername = arg
	} else if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil {
		// Command was sent as a reply — target the replied-to user
		payload.TargetTelegramID = msg.ReplyToMessage.From.ID
	}

	b.worker.Submit(worker.Task{
		Type:    worker.TaskUnseen,
		ChatID:  msg.Chat.ID,
		Payload: payload,
	})
}

// handleConfig submits a task to display the current chat settings.
func (b *Bot) handleConfig(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.worker.Submit(worker.Task{
		Type:   worker.TaskShowConfig,
		ChatID: update.Message.Chat.ID,
	})
}

// buildInlineKeyboard converts our simple InlineButton slices into Telegram's
// InlineKeyboardMarkup type. Returns nil if there are no buttons, which means
// "no keyboard" - Telegram will either not show one (new message) or remove an
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
// We use it to catch callback queries (inline button clicks) and text
// messages that might be responses to pending input prompts.
func (b *Bot) handleDefault(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}

	// Check if this is a reply to a bot prompt (e.g. "Reply with a country code").
	// ReplyToMessage is non-nil only when the user explicitly replies to a message.
	// Combined with HasPendingInput, this ensures we only capture replies to our
	// own prompts - regular group conversation is never forwarded to the worker.
	if update.Message != nil && update.Message.Text != "" && update.Message.ReplyToMessage != nil {
		chatID := update.Message.Chat.ID
		if b.worker.HasPendingInput(chatID) {
			b.worker.Submit(worker.Task{
				Type:   worker.TaskTextInput,
				ChatID: chatID,
				Payload: worker.TextInputPayload{
					ChatID: chatID,
					Text:   update.Message.Text,
				},
			})
		}
	}
}

// handleCallbackQuery processes an inline button click.
// It parses the callback data, submits a task to the worker, and answers
// the callback query so Telegram removes the loading spinner.
func (b *Bot) handleCallbackQuery(ctx context.Context, cq *models.CallbackQuery) {
	if strings.HasPrefix(cq.Data, "config:") {
		b.handleConfigCallback(cq)
		return
	}

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
