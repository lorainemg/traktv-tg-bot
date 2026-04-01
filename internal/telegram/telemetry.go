package telegram

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/loraine/traktv-tg-bot/internal/worker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var telegramTracer = otel.Tracer("bot.telegram")

func traceUpdateMiddleware() bot.Middleware {
	return func(next bot.HandlerFunc) bot.HandlerFunc {
		return func(ctx context.Context, tgBot *bot.Bot, update *models.Update) {
			spanName := "telegram.update"
			if update != nil {
				switch {
				case update.CallbackQuery != nil:
					spanName = "telegram.callback_query"
				case update.Message != nil:
					spanName = "telegram.message"
				}
			}

			ctx, span := telegramTracer.Start(ctx, spanName, trace.WithAttributes(updateTraceAttributes(update)...))
			defer span.End()

			next(ctx, tgBot, update)
		}
	}
}

func updateTraceAttributes(update *models.Update) []attribute.KeyValue {
	if update == nil {
		return nil
	}

	attrs := []attribute.KeyValue{attribute.Int64("telegram.update.id", int64(update.ID))}

	if msg := update.Message; msg != nil {
		attrs = append(attrs,
			attribute.String("telegram.update.type", "message"),
			attribute.Int64("telegram.chat.id", msg.Chat.ID),
			attribute.Int("telegram.message.id", msg.ID),
			attribute.Int("telegram.thread.id", msg.MessageThreadID),
		)
		if msg.From != nil {
			attrs = append(attrs, attribute.Int64("telegram.sender.id", msg.From.ID))
		}
		if msg.ReplyToMessage != nil {
			attrs = append(attrs, attribute.Int("telegram.reply.id", msg.ReplyToMessage.ID))
		}
		if msg.ForwardOrigin != nil {
			attrs = append(attrs, attribute.Bool("telegram.forwarded", true))
			switch msg.ForwardOrigin.Type {
			case models.MessageOriginTypeUser:
				if mo := msg.ForwardOrigin.MessageOriginUser; mo != nil {
					attrs = append(attrs, attribute.Int64("telegram.forward.sender.id", mo.SenderUser.ID))
				}
			case models.MessageOriginTypeChat:
				if mo := msg.ForwardOrigin.MessageOriginChat; mo != nil {
					attrs = append(attrs, attribute.Int64("telegram.forward.chat.id", mo.SenderChat.ID))
				}
			case models.MessageOriginTypeChannel:
				if mo := msg.ForwardOrigin.MessageOriginChannel; mo != nil {
					attrs = append(attrs,
						attribute.Int64("telegram.forward.chat.id", mo.Chat.ID),
						attribute.Int("telegram.forward.id", mo.MessageID),
					)
				}
			case models.MessageOriginTypeHiddenUser:
				attrs = append(attrs, attribute.String("telegram.forward.sender", "hidden_user"))
			}
		}

		if msg.Text != "" {
			attrs = append(attrs,
				attribute.Int("telegram.text.len", len([]rune(msg.Text))),
				attribute.Bool("telegram.text.present", true),
			)
			if cmd := extractCommand(msg.Text); cmd != "" {
				attrs = append(attrs, attribute.String("telegram.command", cmd))
			}
		}

		return attrs
	}

	if cq := update.CallbackQuery; cq != nil {
		attrs = append(attrs,
			attribute.String("telegram.update.type", "callback_query"),
			attribute.String("telegram.callback.id", cq.ID),
			attribute.Int64("telegram.sender.id", cq.From.ID),
		)
		if cq.Data != "" {
			attrs = append(attrs,
				attribute.Int("telegram.callback.data.len", len([]rune(cq.Data))),
				attribute.String("telegram.callback.action", callbackAction(cq.Data)),
			)
		}
		if cq.Message.Message != nil {
			attrs = append(attrs,
				attribute.Int64("telegram.chat.id", cq.Message.Message.Chat.ID),
				attribute.Int("telegram.message.id", cq.Message.Message.ID),
				attribute.Int("telegram.thread.id", cq.Message.Message.MessageThreadID),
			)
		}
	}

	return attrs
}

func extractCommand(text string) string {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return ""
	}
	cmd := parts[0]
	if !strings.HasPrefix(cmd, "/") {
		return ""
	}
	if at := strings.Index(cmd, "@"); at > 0 {
		cmd = cmd[:at]
	}
	return cmd
}

func callbackAction(data string) string {
	action, _, _ := strings.Cut(data, ":")
	if action == "" {
		return "unknown"
	}
	return action
}

func resultCtx(result worker.Result) context.Context {
	if result.Ctx != nil {
		return result.Ctx
	}
	return context.Background()
}

func startSubmitSpan(ctx context.Context, task worker.Task) (context.Context, trace.Span) {
	return telegramTracer.Start(ctx, "telegram.submit_task",
		trace.WithAttributes(
			attribute.String("task.type", task.Type.String()),
			attribute.Int64("chat.id", task.ChatID),
		),
	)
}

func startSendResultSpan(result worker.Result) (context.Context, trace.Span) {
	ctx := resultCtx(result)
	return telegramTracer.Start(ctx, "telegram.send_result",
		trace.WithAttributes(
			attribute.Int64("chat.id", result.ChatID),
			attribute.Int("thread.id", result.ThreadID),
		),
	)
}
