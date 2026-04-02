# Trakt TV Telegram Bot

A Telegram bot that keeps your group chat in sync with what everyone's watching. It posts notifications when new episodes air, tracks who's seen what, and syncs watch status with [Trakt.tv](https://trakt.tv) -- all without leaving Telegram.

Built in Go as a learning project. No third-party Trakt wrapper libraries -- just `net/http` and the Trakt REST API.

## What it does

When a new episode airs for any show your group follows, the bot posts a notification like this:

```
📺 Severance · S02E05
  Trojan's Horse · ⏱ 45m

🗓 Mar 14 at 9:00 PM EST
⭐️ 9.2 Trakt · IMDb
📡 Apple TV+

▶️ Stremio · 🔗 Where to Watch

Watched by: @alice ✅  @bob ⏳

  [ ✅ Watched ]  [ ↩️ Unwatched ]
```

Click **Watched** and the bot marks it on Trakt and updates the message for everyone. Watch directly on Trakt? The bot picks that up too.

Once the whole group has watched, the notification can auto-delete to keep the chat clean.

## Features

- **Episode notifications** -- automatic alerts when episodes air for followed shows, with ratings, runtime, streaming links, and Stremio deep links
- **Two-way Trakt sync** -- mark watched/unwatched from Telegram buttons or watch on Trakt and the bot updates the notification
- **Group watch tracking** -- see who's watched and who hasn't on every notification
- **Auto-cleanup** -- optionally delete notifications after everyone's caught up
- **Forum topic routing** -- route notifications by genre to different forum topics (e.g. anime goes to the Anime topic)
- **Per-chat config** -- country, timezone, notification window, and auto-delete preferences via inline buttons
- **Trakt OAuth** -- device flow auth, no web server needed

## Commands

| Command | Description |
|---|---|
| `/sub` | Link your Trakt account and subscribe to notifications |
| `/unsub` | Pause notifications (re-subscribe anytime) |
| `/upcoming [days]` | See what's airing soon (default 7, max 31) |
| `/unseen [@user]` | Unseen episode counts -- yours or someone else's |
| `/shows [@user]` | List returning shows you or someone follows |
| `/whowatch <show>` | See who in the chat watches a specific show |
| `/register_topic <genre>` | Route a genre's notifications to this forum topic |
| `/config` | Chat settings: country, timezone, auto-delete, notify window |
| `/help` | Full command reference |

## Architecture

```
cmd/bot/main.go              <- wiring + scheduled tickers
internal/
  telegram/bot.go            <- thin UI layer: commands + message delivery
  worker/
    worker.go                <- channel-based task queue + dispatch loop
    episodes.go              <- episode check handler
    watch.go, watch_history  <- watched/unwatched + Trakt history sync
    sub.go, unsub.go         <- Trakt OAuth device flow + subscribe/unsub
    upcoming.go, shows.go    <- /upcoming, /shows with pagination
    unseen.go, whowatch.go   <- /unseen, /whowatch handlers
    config.go, topics.go     <- per-chat settings + forum topic routing
    format.go                <- message formatting + pagination helpers
    deletions.go             <- scheduled message cleanup
    token.go                 <- OAuth token refresh
  trakt/client.go            <- HTTP client for Trakt.tv REST API
  tmdb/client.go             <- TMDB API for streaming provider lookups
  storage/
    service.go               <- Service interface (decouples DB from logic)
    postgres.go              <- GORM PostgreSQL implementation
    models.go                <- User, Notification, WatchStatus, ChatConfig
  otel/                      <- OpenTelemetry tracing setup
```

**Data flow:** Telegram commands become `Task` structs sent through a buffered channel to the worker. The worker processes them and sends `Result` structs back through another channel. A forwarder goroutine reads results and delivers them as Telegram messages. Background tickers submit episode checks, watch history syncs, and deletion processing on a schedule.

**No circular dependencies:** `telegram` only knows about `worker`. `worker` depends on `storage.Service` (interface) and `trakt.Client`. The telegram package never imports storage or trakt.

## Setup

### Prerequisites

- Go 1.26+
- PostgreSQL
- A [Trakt.tv API app](https://trakt.tv/oauth/applications) (client ID + secret)
- A [Telegram bot token](https://core.telegram.org/bots#botfather)
- A [TMDB API key](https://www.themoviedb.org/settings/api) (for streaming provider info)

### Environment variables

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram bot token from BotFather |
| `DATABASE_URL` | PostgreSQL connection string |
| `TRAKT_CLIENT_ID` | Trakt API client ID |
| `TRAKT_CLIENT_SECRET` | Trakt API client secret |
| `TMDB_API_KEY` | TMDB API key |
| `ENV` | Set to `dev` for debug logging and 1-minute tick intervals |

### Run with Docker (recommended)

The project uses [.NET Aspire](https://learn.microsoft.com/en-us/dotnet/aspire/) to orchestrate the bot and PostgreSQL containers. In development mode, source code is bind-mounted and [Air](https://github.com/air-verse/air) handles hot reloading.

```bash
# Development (hot reload, 1-min ticks, debug logs)
aspire run

# Production build
aspire do push-and-prepare-env --environment production
```

### Run directly

```bash
export TELEGRAM_BOT_TOKEN="..."
export DATABASE_URL="postgres://user:pass@localhost:5432/trakt?sslmode=disable"
export TRAKT_CLIENT_ID="..."
export TRAKT_CLIENT_SECRET="..."
export TMDB_API_KEY="..."

go run ./cmd/bot
```

## CI/CD

GitHub Actions workflow at `.github/workflows/deploy-main.yml` runs on every push to `main`:

1. Builds and pushes the Docker image via Aspire
2. Deploys the compose stack to Portainer

Required repository secrets: `PORTAINER_URL`, `PORTAINER_API_TOKEN`, `PORTAINER_ENDPOINT_ID`, `PORTAINER_STACK_ID`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `TELEGRAM_BOT_TOKEN`, `TRAKT_CLIENT_ID`, `TRAKT_CLIENT_SECRET`, `TMDB_API_KEY`.

## Observability

The bot exports OpenTelemetry traces for every worker task and HTTP request to the Trakt/TMDB APIs. In development with Aspire, traces are available in the Aspire dashboard.

## License

[MIT](LICENSE)
