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

func (b *Bot) submit(ctx context.Context, task worker.Task) {
	taskCtx, span := startSubmitSpan(ctx, task)
	task.Ctx = taskCtx
	b.worker.Submit(task)
	span.End()
}

// NewBot creates and configures a Telegram bot with command handlers.
func NewBot(token string, w *worker.Worker) (*Bot, error) {
	b := &Bot{
		worker: w,
	}

	opts := []bot.Option{
		withInstrumentedHTTPClientOption(),
		bot.WithMiddlewares(traceUpdateMiddleware()),
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
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/sub", bot.MatchTypePrefix, b.handleSub)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/unsub", bot.MatchTypePrefix, b.handleUnsub)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/register_topic", bot.MatchTypePrefix, b.handleRegisterTopic)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/upcoming", bot.MatchTypePrefix, b.handleUpcoming)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/shows", bot.MatchTypePrefix, b.handleShows)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/config", bot.MatchTypePrefix, b.handleConfig)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/unseen", bot.MatchTypePrefix, b.handleUnseen)
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/whowatch", bot.MatchTypePrefix, b.handleWhoWatch)

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
	ctx, span := startSendResultSpan(result)
	defer span.End()
	result.Ctx = ctx

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
	_, err := b.bot.AnswerCallbackQuery(resultCtx(result), &bot.AnswerCallbackQueryParams{
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
	_, err := b.bot.DeleteMessage(resultCtx(result), &bot.DeleteMessageParams{
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
	// ReplyParameters makes this message a reply to another message.
	// AllowSendingWithoutReply lets the message send even if the original
	// was deleted - avoids a "message not found" error from Telegram.
	if result.ReplyToMessageID != 0 {
		params.ReplyParameters = &models.ReplyParameters{
			MessageID:                result.ReplyToMessageID,
			AllowSendingWithoutReply: true,
		}
	}
	// Set ReplyMarkup: ForceReply takes priority (prompts user to reply),
	// otherwise use inline keyboard buttons if present.
	// A nil pointer assigned to the ReplyMarkup interface field is non-nil in Go,
	// which causes Telegram to reject the request - so we only set it when needed.
	if result.ForceReply {
		params.ReplyMarkup = &models.ForceReply{
			ForceReply:            true,
			Selective:             result.Selective,
			InputFieldPlaceholder: result.InputFieldPlaceholder,
		}
	} else if kb := buildInlineKeyboard(result.InlineButtons); kb != nil {
		params.ReplyMarkup = kb
	}
	msg, err := b.bot.SendMessage(resultCtx(result), params)
	if err != nil {
		slog.Error("failed to send message", "error", err, "chat_id", result.ChatID, "text", result.Text)
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
	// Preserve the original thumbnail by passing the same PhotoURL,
	// otherwise disable link previews to avoid Telegram auto-generating
	// a preview from mention links or Trakt URLs in the message.
	var preview *models.LinkPreviewOptions
	if result.PhotoURL != "" {
		preview = &models.LinkPreviewOptions{
			URL:              &result.PhotoURL,
			PreferLargeMedia: bot.False(),
			ShowAboveText:    bot.True(),
		}
	} else {
		preview = &models.LinkPreviewOptions{
			IsDisabled: bot.True(),
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
	_, err := b.bot.EditMessageText(resultCtx(result), editParams)
	if err != nil {
		// Telegram rejects edits when the content hasn't changed - this is expected
		// when a user clicks the button but was already marked as watched.
		if strings.Contains(err.Error(), "message is not modified") {
			return
		}
		slog.Error("failed to edit message", "error", err, "chat_id", result.ChatID, "message_id", result.EditMessageID, "text", result.Text)
	}
}

// StartResultsForwarder launches a background goroutine that reads Results
// from the worker's output channel and delivers them as Telegram messages.
func (b *Bot) StartResultsForwarder(ctx context.Context) {
	go func() {
		for {
			select {
			case result := <-b.worker.Results():
				if result.Ctx == nil {
					result.Ctx = ctx
				}
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
• Each notification tracks who's watched - click "✅ Watched" to update your status, or "↩️ Unwatched" to undo it (both sync to your Trakt account)
• If you watch on Trakt directly, I'll pick that up too
• Notifications can auto-delete once everyone's watched (toggle via /config)

<b>Commands</b>
/sub - Link your <a href="https://trakt.tv">Trakt.tv</a> account and subscribe to notifications
/unsub - Pause episode notifications (re-subscribe anytime with /sub)
/upcoming [days] - See what's airing soon (default: 7 days, max: 31)
/unseen [@user] - See unseen episode counts (yours, or reply/mention someone)
/shows [@user] - List returning shows you or someone else follows
/whowatch &lt;show&gt; - Check who in this chat watches a specific show
/register_topic &lt;genre&gt; - Route episode notifications of a genre to this group topic
/config - Chat settings: country, timezone, auto-delete watched notifications, notify window

Just /sub to get started and I'll handle the rest!`

	_, err := tgBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:             update.Message.Chat.ID,
		MessageThreadID:    update.Message.MessageThreadID,
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
		MessageThreadID:    update.Message.MessageThreadID,
		Text:               "Hey! I'm your TV show companion. I'll keep you posted when new episodes air and track what everyone's watching.\n\nGet started by linking your [Trakt.tv](https://trakt.tv) account with /sub, or check /help to see everything I can do.",
		ParseMode:          models.ParseModeMarkdownV1,
		LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: bot.True()},
	})
	if err != nil {
		slog.Error("failed to send message", "error", err)
	}
}

// handleSub submits a subscribe task to the worker. For new users this starts
// the Trakt OAuth device flow; for existing users it re-subscribes (unmutes).
func (b *Bot) handleSub(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.submit(ctx, worker.Task{
		Type:     worker.TaskSub,
		ChatID:   update.Message.Chat.ID,
		ThreadID: update.Message.MessageThreadID,
		Payload: worker.SubPayload{
			TelegramID: update.Message.From.ID,
			ChatID:     update.Message.Chat.ID,
			FirstName:  update.Message.From.FirstName,
			Username:   update.Message.From.Username,
			MessageID:  update.Message.ID,
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

	b.submit(ctx, worker.Task{
		Type:     worker.TaskRegisterTopic,
		ChatID:   msg.Chat.ID,
		ThreadID: msg.MessageThreadID,
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

	b.submit(ctx, worker.Task{
		Type:     worker.TaskUpcoming,
		ChatID:   update.Message.Chat.ID,
		ThreadID: update.Message.MessageThreadID,
		Payload:  days,
	})
}

// handleShows submits a task to list all followed shows in this chat.
func (b *Bot) handleShows(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	msg := update.Message
	b.submit(ctx, worker.Task{
		Type:     worker.TaskShows,
		ChatID:   msg.Chat.ID,
		ThreadID: msg.MessageThreadID,
		Payload:  parseUserTarget(msg),
	})
}

// handleUnsub submits a task to pause episode notifications for the calling user.
func (b *Bot) handleUnsub(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.submit(ctx, worker.Task{
		Type:     worker.TaskUnsub,
		ChatID:   update.Message.Chat.ID,
		ThreadID: update.Message.MessageThreadID,
		Payload: worker.UnsubPayload{
			TelegramID: update.Message.From.ID,
			ChatID:     update.Message.Chat.ID,
		},
	})
}

// handleConfigCallback routes config inline button clicks to the appropriate task.
func (b *Bot) handleConfigCallback(ctx context.Context, cq *models.CallbackQuery) {
	action := strings.TrimPrefix(cq.Data, "config:")
	threadID := cq.Message.Message.MessageThreadID
	payload := worker.ConfigCallbackPayload{
		ChatID:          cq.Message.Message.Chat.ID,
		CallbackQueryID: cq.ID,
		MessageID:       cq.Message.Message.ID,
		UserTelegramID:  cq.From.ID,
	}

	switch {
	case action == "delete":
		b.submit(ctx, worker.Task{
			Type:     worker.TaskToggleDeleteWatched,
			ChatID:   payload.ChatID,
			ThreadID: threadID,
			Payload:  payload,
		})
	case action == "country":
		b.submit(ctx, worker.Task{
			Type:     worker.TaskPromptCountry,
			ChatID:   payload.ChatID,
			ThreadID: threadID,
			Payload:  payload,
		})
	case action == "notify":
		b.submit(ctx, worker.Task{
			Type:     worker.TaskPromptNotifyHours,
			ChatID:   payload.ChatID,
			ThreadID: threadID,
			Payload:  payload,
		})
	case action == "timezone":
		b.submit(ctx, worker.Task{
			Type:     worker.TaskShowTimezones,
			ChatID:   payload.ChatID,
			ThreadID: threadID,
			Payload:  payload,
		})
	case strings.HasPrefix(action, "tz:"):
		// User picked a specific timezone from the button list.
		// Callback data format: "config:tz:America/New_York"
		b.submit(ctx, worker.Task{
			Type:     worker.TaskSetTimezone,
			ChatID:   cq.Message.Message.Chat.ID,
			ThreadID: threadID,
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
	b.submit(ctx, worker.Task{
		Type:     worker.TaskUnseen,
		ChatID:   msg.Chat.ID,
		ThreadID: msg.MessageThreadID,
		Payload:  worker.UnseenPayload{UserTarget: parseUserTarget(msg)},
	})
}

// handleWhoWatch parses a show name and submits a task to check which
// chat members watch that show.
// Usage: /whowatch breaking bad
func (b *Bot) handleWhoWatch(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	msg := update.Message

	_, query, _ := strings.Cut(msg.Text, " ")
	query = strings.TrimSpace(query)
	if query == "" {
		_, _ = tgBot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:          msg.Chat.ID,
			MessageThreadID: msg.MessageThreadID,
			Text:            "Usage: /whowatch <show name>\nExample: /whowatch breaking bad",
		})
		return
	}

	b.submit(ctx, worker.Task{
		Type:     worker.TaskWhoWatches,
		ChatID:   msg.Chat.ID,
		ThreadID: msg.MessageThreadID,
		Payload:  worker.WhoWatchesPayload{Query: query},
	})
}

// parseUserTarget extracts the target user from a command message.
// Supports three forms: "/cmd" (self), "/cmd @username", or reply to a message.
// Reused by any command that targets a single user (/shows, /unseen, etc.).
func parseUserTarget(msg *models.Message) worker.UserTarget {
	target := worker.UserTarget{RequesterID: msg.From.ID}

	// Check for @username argument: "/cmd @loraine" or "/cmd loraine"
	_, arg, _ := strings.Cut(msg.Text, " ")
	arg = strings.TrimSpace(arg)
	arg = strings.TrimPrefix(arg, "@") // strip leading @ if present

	if arg != "" {
		target.TargetUsername = arg
	} else if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil &&
		msg.ReplyToMessage.ForumTopicCreated == nil {
		// Command was sent as a reply — target the replied-to user.
		// The ForumTopicCreated check filters out the service message that
		// Telegram auto-sets as ReplyToMessage for every message in a topic.
		target.TargetTelegramID = msg.ReplyToMessage.From.ID
	}

	return target
}

// handleConfig submits a task to display the current chat settings.
func (b *Bot) handleConfig(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
	b.submit(ctx, worker.Task{
		Type:     worker.TaskShowConfig,
		ChatID:   update.Message.Chat.ID,
		ThreadID: update.Message.MessageThreadID,
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
			b.submit(ctx, worker.Task{
				Type:     worker.TaskTextInput,
				ChatID:   chatID,
				ThreadID: update.Message.MessageThreadID,
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
	// "noop" is the page indicator button (e.g. "2/3") — just dismiss the spinner.
	if cq.Data == "noop" {
		b.bot.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cq.ID,
		})
		return
	}

	if strings.HasPrefix(cq.Data, "config:") {
		b.handleConfigCallback(ctx, cq)
		return
	}

	if strings.HasPrefix(cq.Data, "watched:") || strings.HasPrefix(cq.Data, "unwatched:") {
		b.handleWatchCallback(ctx, cq)
		return
	}

	// Pagination callbacks: "shows:<page>" or "upcoming:<days>:<page>"
	if strings.HasPrefix(cq.Data, "shows:") {
		b.handlePaginationCallback(ctx, cq, worker.TaskShowsPage)
		return
	}
	if strings.HasPrefix(cq.Data, "upcoming:") {
		b.handlePaginationCallback(ctx, cq, worker.TaskUpcomingPage)
		return
	}
}

// handleWatchCallback parses "watched:<id>" and "unwatched:<id>" callbacks
// and submits the appropriate task. Both share the same payload structure.
func (b *Bot) handleWatchCallback(ctx context.Context, cq *models.CallbackQuery) {
	// Determine direction by checking the prefix, then trim it to get the ID.
	taskType := worker.TaskMarkWatched
	idStr := strings.TrimPrefix(cq.Data, "watched:")
	if strings.HasPrefix(cq.Data, "unwatched:") {
		taskType = worker.TaskMarkUnwatched
		idStr = strings.TrimPrefix(cq.Data, "unwatched:")
	}

	notificationID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return
	}

	b.submit(ctx, worker.Task{
		Type:     taskType,
		ChatID:   cq.Message.Message.Chat.ID,
		ThreadID: cq.Message.Message.MessageThreadID,
		Payload: worker.WatchActionPayload{
			TelegramID:      cq.From.ID,
			ChatID:          cq.Message.Message.Chat.ID,
			NotificationID:  uint(notificationID),
			CallbackQueryID: cq.ID,
		},
	})
}

// handlePaginationCallback parses a pagination callback and submits the
// appropriate page task. Callback data formats:
//   - "shows:<telegramID>:<page>"  → TaskShowsPage
//   - "upcoming:<days>:<page>"     → TaskUpcomingPage
func (b *Bot) handlePaginationCallback(ctx context.Context, cq *models.CallbackQuery, taskType worker.TaskType) {
	// Split callback data into parts: ["shows", "123", "1"] or ["upcoming", "7", "2"]
	parts := strings.Split(cq.Data, ":")

	payload := worker.PagePayload{
		ChatID:          cq.Message.Message.Chat.ID,
		CallbackQueryID: cq.ID,
		MessageID:       cq.Message.Message.ID,
	}

	switch taskType {
	case worker.TaskShowsPage:
		// Format: "shows:<telegramID>:<page>"
		if len(parts) != 3 {
			return
		}
		telegramID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return
		}
		page, err := strconv.Atoi(parts[2])
		if err != nil {
			return
		}
		payload.TargetTelegramID = telegramID
		payload.Page = page

	case worker.TaskUpcomingPage:
		// Format: "upcoming:<days>:<page>"
		if len(parts) != 3 {
			return
		}
		days, err := strconv.Atoi(parts[1])
		if err != nil {
			return
		}
		page, err := strconv.Atoi(parts[2])
		if err != nil {
			return
		}
		payload.Days = days
		payload.Page = page
	}

	b.submit(ctx, worker.Task{
		Type:     taskType,
		ChatID:   cq.Message.Message.Chat.ID,
		ThreadID: cq.Message.Message.MessageThreadID,
		Payload:  payload,
	})
}
