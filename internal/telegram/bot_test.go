package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/loraine/traktv-tg-bot/internal/worker"
	"github.com/stretchr/testify/assert"
)

func TestParseUserTarget(t *testing.T) {
	tests := []struct {
		name     string
		msg      *models.Message
		expected worker.UserTarget
	}{
		{
			name: "extracts @username argument",
			msg: &models.Message{
				Text: "/unseen @loraine",
				From: &models.User{ID: 111},
			},
			expected: worker.UserTarget{
				RequesterID:    111,
				TargetUsername: "loraine",
			},
		},
		{
			name: "extracts username without @ prefix",
			msg: &models.Message{
				Text: "/unseen loraine",
				From: &models.User{ID: 111},
			},
			expected: worker.UserTarget{
				RequesterID:    111,
				TargetUsername: "loraine",
			},
		},
		{
			name: "no argument and no reply targets requester only",
			msg: &models.Message{
				Text: "/unseen",
				From: &models.User{ID: 111},
			},
			expected: worker.UserTarget{
				RequesterID: 111,
			},
		},
		{
			name: "replying to another user's message",
			msg: &models.Message{
				Text:           "/unseen",
				From:           &models.User{ID: 111},
				ReplyToMessage: &models.Message{From: &models.User{ID: 222}},
			},
			expected: worker.UserTarget{
				RequesterID:      111,
				TargetTelegramID: 222,
			},
		},
		{
			name: "ignores reply to forum topic service message",
			msg: &models.Message{
				Text:           "/unseen",
				From:           &models.User{ID: 111},
				ReplyToMessage: &models.Message{From: &models.User{ID: 222}, ForumTopicCreated: &models.ForumTopicCreated{Name: "topic"}},
			},
			expected: worker.UserTarget{
				RequesterID: 111,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseUserTarget(tt.msg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildInlineKeyboard(t *testing.T) {
	t.Run("returns nil for empty buttons", func(t *testing.T) {
		assert.Nil(t, buildInlineKeyboard(nil))
		assert.Nil(t, buildInlineKeyboard([][]worker.InlineButton{}))
	})

	t.Run("converts buttons to Telegram keyboard", func(t *testing.T) {
		buttons := [][]worker.InlineButton{
			{
				{Text: "Yes", CallbackData: "confirm"},
				{Text: "No", CallbackData: "cancel"},
			},
		}

		result := buildInlineKeyboard(buttons)

		assert.NotNil(t, result)
		assert.Len(t, result.InlineKeyboard, 1)    // one row
		assert.Len(t, result.InlineKeyboard[0], 2) // two buttons in the row
		assert.Equal(t, "Yes", result.InlineKeyboard[0][0].Text)
		assert.Equal(t, "confirm", result.InlineKeyboard[0][0].CallbackData)
		assert.Equal(t, "No", result.InlineKeyboard[0][1].Text)
	})
}
