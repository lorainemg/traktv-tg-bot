package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMentionLink(t *testing.T) {
	t.Run("uses @username with t.me link when username is set", func(t *testing.T) {
		user := &User{Username: "loraine", TelegramID: 111}
		assert.Equal(t, "[@loraine](https://t.me/loraine)", user.MentionLink())
	})

	t.Run("falls back to FirstName with tg:// deep link", func(t *testing.T) {
		user := &User{FirstName: "Loraine", TelegramID: 111}
		assert.Equal(t, "[Loraine](tg://user?id=111)", user.MentionLink())
	})

	t.Run("uses 'User' when both username and first name are empty", func(t *testing.T) {
		user := &User{TelegramID: 111}
		assert.Equal(t, "[User](tg://user?id=111)", user.MentionLink())
	})
}
