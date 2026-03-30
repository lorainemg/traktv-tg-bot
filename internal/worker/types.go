package worker

// TaskType identifies what kind of work a task represents.
// Using a custom type instead of raw int makes the code self-documenting.
type TaskType int

// iota is Go's auto-incrementing constant generator.
// It starts at 0 and increments by 1 for each constant in the block —
// like an auto-numbered enum in C#.
const (
	TaskCheckEpisodes     TaskType = iota // = 0
	TaskStartAuth                        // = 1
	TaskRegisterTopic                    // = 2
	TaskSetMuted                         // = 3
	TaskMarkWatched                      // = 4
	TaskCheckWatchHistory                // = 5
	TaskProcessDeletions                 // = 6
	TaskUpcoming                         // = 7
)

// Task represents a unit of work submitted to the worker queue.
type Task struct {
	Type    TaskType
	ChatID  int64 // where to send Telegram responses
	Payload any   // extra data, varies by task type (like interface{} — accepts any value)
}

// InlineButton describes a single inline keyboard button.
// The worker builds these; the telegram package converts them to Telegram API types.
type InlineButton struct {
	Text         string // label shown on the button
	CallbackData string // hidden payload sent back when the button is clicked (max 64 bytes)
}

// Result represents a message the worker wants delivered via Telegram.
// The worker never talks to Telegram directly — it puts Results on a channel,
// and the Telegram side reads and sends them.
type Result struct {
	ChatID   int64
	ThreadID int // forum topic thread ID — 0 means send to the default/general topic
	Text     string
	PhotoURL string // if set, message is sent as a photo with Text as caption
	OnSent   func(messageID int) error

	// EditMessageID, when non-zero, tells the bot to edit an existing message
	// instead of sending a new one. Zero value (default) means "send new message".
	EditMessageID int

	// DeleteMessageID, when non-zero, tells the bot to delete a message.
	// Takes priority over EditMessageID and sending — if set, only deletion happens.
	DeleteMessageID int

	// CallbackQueryID, when non-empty, tells the bot to answer a callback query
	// (show a toast/popup) instead of sending or editing a message.
	CallbackQueryID string
	// CallbackShowAlert controls the toast style: false = brief top-bar toast,
	// true = modal popup the user must dismiss. Useful for errors/warnings.
	CallbackShowAlert bool

	// InlineButtons is a 2D slice: each inner slice is one row of buttons.
	// nil means no keyboard attached.
	InlineButtons [][]InlineButton
}

// AuthPayload carries the data needed to start the Trakt OAuth device flow.
type AuthPayload struct {
	TelegramID int64
	ChatID     int64  // the chat where the user ran /auth — notifications go here
	FirstName  string // user's Telegram display name — used in farewell messages
	Username   string
}

// MutePayload carries the data needed to mute or unmute a user.
type MutePayload struct {
	TelegramID int64
	ChatID     int64
	Muted      bool // true = mute (stop notifications), false = unmute (resume)
}

// TopicPayload carries the data needed to register a forum topic.
type TopicPayload struct {
	ChatID   int64
	ThreadID int
	Name     string // user-provided topic name, e.g. "anime"
}

// MarkWatchedPayload carries the data needed to mark an episode as watched on Trakt.
// NotificationID comes directly from the inline button's callback data.
type MarkWatchedPayload struct {
	TelegramID      int64  // the user who clicked — used to find their Trakt token
	ChatID          int64  // where to send the confirmation message
	NotificationID  uint   // DB ID of the notification — used to find which episode
	CallbackQueryID string // Telegram callback query ID — used to answer with a toast
}
