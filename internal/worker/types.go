package worker

// TaskType identifies what kind of work a task represents.
// Using a custom type instead of raw int makes the code self-documenting.
type TaskType int

// iota is Go's auto-incrementing constant generator.
// It starts at 0 and increments by 1 for each constant in the block -
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
	TaskShows                            // = 8
	TaskShowConfig                       // = 9
	TaskToggleDeleteWatched              // = 10
	TaskTextInput                        // = 11
	TaskPromptCountry                    // = 12
	TaskShowTimezones                    // = 13
	TaskSetTimezone                      // = 14
	TaskUnseen                           // = 15
	TaskShowsPage                        // = 16 — paginated /shows callback
	TaskUpcomingPage                     // = 17 — paginated /upcoming callback
	TaskWhoWatches                       // = 18 — check which users watch a show
)

// Task represents a unit of work submitted to the worker queue.
type Task struct {
	Type     TaskType
	ChatID   int64 // where to send Telegram responses
	ThreadID int   // forum topic thread ID - 0 means General/default topic
	Payload  any   // extra data, varies by task type (like interface{} - accepts any value)
}

// TextResult builds a Result pre-filled with the task's ChatID and ThreadID.
// Covers the common case where a handler just needs to send a text reply
// back to the same chat and topic the command came from.
func (t Task) TextResult(text string) Result {
	return Result{
		ChatID:   t.ChatID,
		ThreadID: t.ThreadID,
		Text:     text,
	}
}

// InlineButton describes a single inline keyboard button.
// The worker builds these; the telegram package converts them to Telegram API types.
type InlineButton struct {
	Text         string // label shown on the button
	CallbackData string // hidden payload sent back when the button is clicked (max 64 bytes)
}

// Result represents a message the worker wants delivered via Telegram.
// The worker never talks to Telegram directly - it puts Results on a channel,
// and the Telegram side reads and sends them.
type Result struct {
	ChatID   int64
	ThreadID int // forum topic thread ID - 0 means send to the default/general topic
	Text     string
	PhotoURL string // if set, message is sent as a photo with Text as caption
	OnSent   func(messageID int) error

	// EditMessageID, when non-zero, tells the bot to edit an existing message
	// instead of sending a new one. Zero value (default) means "send new message".
	EditMessageID int

	// DeleteMessageID, when non-zero, tells the bot to delete a message.
	// Takes priority over EditMessageID and sending - if set, only deletion happens.
	DeleteMessageID int

	// CallbackQueryID, when non-empty, tells the bot to answer a callback query
	// (show a toast/popup) instead of sending or editing a message.
	CallbackQueryID string
	// CallbackShowAlert controls the toast style: false = brief top-bar toast,
	// true = modal popup the user must dismiss. Useful for errors/warnings.
	CallbackShowAlert bool

	// ForceReply, when true, tells Telegram to show a reply UI to the user.
	// Used for prompts that expect text input - the reply ensures the bot
	// receives the message even in group chats with privacy mode enabled.
	ForceReply            bool
	Selective             bool   // when true, ForceReply only targets users mentioned in the text
	InputFieldPlaceholder string // hint shown in the input field, e.g. "US, GB, BR"

	// InlineButtons is a 2D slice: each inner slice is one row of buttons.
	// nil means no keyboard attached.
	InlineButtons [][]InlineButton
}

// AuthPayload carries the data needed to start the Trakt OAuth device flow.
type AuthPayload struct {
	TelegramID int64
	ChatID     int64  // the chat where the user ran /auth - notifications go here
	FirstName  string // user's Telegram display name - used in farewell messages
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

// ConfigCallbackPayload carries data for config inline button callbacks.
// Reused by all config buttons (toggle delete, change country, change timezone).
type ConfigCallbackPayload struct {
	ChatID          int64
	CallbackQueryID string
	MessageID       int   // the config message to edit in-place after changes
	UserTelegramID  int64 // who clicked the button - used for selective ForceReply
}

// TimezonePayload carries the selected IANA timezone from an inline button callback.
type TimezonePayload struct {
	ChatID          int64
	CallbackQueryID string
	MessageID       int    // the config message to edit after saving
	Timezone        string // IANA timezone name, e.g. "America/New_York"
}

// TextInputPayload carries a plain text message that the user sent in response
// to a pending input prompt (e.g. typing a country code after clicking "Change Country").
type TextInputPayload struct {
	ChatID int64
	Text   string // the raw text the user sent
}

// UserTarget identifies which user a command is directed at.
// Several commands (/shows, /unseen) support the same three forms:
//   - no argument        → RequesterID (the caller themselves)
//   - @username argument → TargetUsername
//   - reply to a message → TargetTelegramID
//
// Struct embedding (below) lets other payload types inherit these fields
// without repeating them — similar to composition in C# or mixins in Python.
type UserTarget struct {
	RequesterID      int64  // who ran the command — used as fallback target
	TargetTelegramID int64  // set when replying to another user's message
	TargetUsername   string // set when using @username argument
}

// UnseenPayload carries the data needed to look up unseen episodes.
// Embedding UserTarget promotes its fields — you can access
// payload.RequesterID directly, as if it were declared here.
type UnseenPayload struct {
	UserTarget
}

// PagePayload carries data for paginated list callbacks (shows/upcoming).
// When the user clicks a Prev/Next button, the bot sends this so the worker
// knows which page to render and which message to edit.
type PagePayload struct {
	ChatID           int64
	CallbackQueryID  string
	MessageID        int   // the existing message to edit in-place
	Page             int   // zero-based page index
	TargetTelegramID int64 // only used for /shows — whose shows to display
	Days             int   // only used for /upcoming — the look-ahead window in days
}

// WhoWatchesPayload carries the data needed to check which chat members watch a show.
// ChatID and ThreadID come from the Task struct itself (used by TextResult),
// so this payload only needs the search query.
type WhoWatchesPayload struct {
	Query string // the show name the user typed, e.g. "breaking bad"
}

// MarkWatchedPayload carries the data needed to mark an episode as watched on Trakt.
// NotificationID comes directly from the inline button's callback data.
type MarkWatchedPayload struct {
	TelegramID      int64  // the user who clicked - used to find their Trakt token
	ChatID          int64  // where to send the confirmation message
	NotificationID  uint   // DB ID of the notification - used to find which episode
	CallbackQueryID string // Telegram callback query ID - used to answer with a toast
}
