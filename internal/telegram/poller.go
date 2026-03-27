package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"text/template"
	"time"

	"github.com/go-telegram/bot"
	"github.com/loraine/traktv-tg-bot/internal/storage"
	"github.com/loraine/traktv-tg-bot/internal/trakt"
	"gorm.io/gorm"
)

// episodeData holds the fields available in the notification template.
type episodeData struct {
	ShowTitle    string
	EpisodeKey   string
	EpisodeTitle string
	FirstAired   string
}

// episodeTemplate defines the message format for new episode notifications.
// template.Must panics if the template fails to parse — safe here because
// the template is a constant defined at compile time.
// {{.FieldName}} references fields from episodeData — like Jinja2's {{ field }}.
var episodeTemplate = template.Must(template.New("episode").Parse(
	`📺 {{.ShowTitle}}
{{.EpisodeKey}} — "{{.EpisodeTitle}}"
Aired: {{.FirstAired}}`,
))

// formatAirDate parses a Trakt ISO timestamp and returns a human-friendly date.
// time.Parse takes a layout string using Go's reference date: Mon Jan 2 15:04:05 MST 2006.
// "2006-01-02T15:04:05.000Z" matches the Trakt format — each number is a specific
// component of the reference date (2006=year, 01=month, 02=day, 15=hour, etc.).
func formatAirDate(isoDate string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", isoDate)
	if err != nil {
		return isoDate // fallback to raw string if parsing fails
	}
	// "January 2, 2006" uses the same reference date to define the output format.
	// 3:04 PM uses the reference time: 3=hour(12h), 04=minute, PM=am/pm marker
	return t.Format("January 2, 2006 at 3:04 PM")
}

// StartPoller launches a background goroutine that periodically checks
// for new episodes across all linked users and sends notifications.
func StartPoller(ctx context.Context, tgBot *bot.Bot, db *gorm.DB, traktClient *trakt.Client, chatID int64, interval time.Duration) {
	// go func() launches this as a background goroutine.
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run once immediately on startup, then on every tick.
		checkNewEpisodes(ctx, tgBot, db, traktClient, chatID)

		// "for { select { ... } }" is Go's way of waiting on multiple events.
		// select blocks until one of its cases is ready — like a switch for channels.
		for {
			select {
			case <-ticker.C:
				// Ticker fired — time to check for new episodes.
				checkNewEpisodes(ctx, tgBot, db, traktClient, chatID)
			case <-ctx.Done():
				// Context was cancelled (Ctrl+C / shutdown) — stop the poller.
				fmt.Println("Poller stopped.")
				return
			}
		}
	}()
}

// checkNewEpisodes loads all linked users, fetches today's calendar for each,
// and sends a notification for any episodes not yet reported.
func checkNewEpisodes(ctx context.Context, tgBot *bot.Bot, db *gorm.DB, traktClient *trakt.Client, chatID int64) {
	fmt.Println("Checking for new episodes...")

	var users []storage.User
	db.Find(&users)
	today := time.Now().Format("2006-01-02")
	for _, user := range users {
		entries, err := traktClient.GetCalendar(user.TraktAccessToken, today, 1)
		if err != nil {
			fmt.Println("Error fetching calendar:", err)
			continue
		}
		fmt.Printf("Found %d episodes for user %d\n", len(entries), user.ID)
		for _, entry := range entries {
			episodeKey := fmt.Sprintf("S%02dE%02d", entry.Episode.Season, entry.Episode.Number)

			// Check if we already sent this notification
			var notification storage.Notification
			err = db.Where(
				"user_id = ? AND show_title = ? AND episode_key = ?",
				user.ID, entry.Show.Title, episodeKey,
			).First(&notification).Error
			
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// bytes.Buffer is a growable byte buffer that implements io.Writer.
				// The template writes into it, then we extract the string.
				var buf bytes.Buffer
				if err := episodeTemplate.Execute(&buf, episodeData{
					ShowTitle:    entry.Show.Title,
					EpisodeKey:   episodeKey,
					EpisodeTitle: entry.Episode.Title,
					FirstAired:   formatAirDate(entry.FirstAired),
				}); err != nil {
					fmt.Println("Error rendering template:", err)
					continue
				}

				_, err = tgBot.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   buf.String(),
				})
				if err != nil {
					fmt.Println("Error sending message:", err)
					continue
				}
				db.Create(&storage.Notification{UserID: user.ID, ShowTitle: entry.Show.Title, EpisodeKey: episodeKey})
			}
		}

	}
}
