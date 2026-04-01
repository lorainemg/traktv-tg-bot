# OpenTelemetry Integration Notes

## What Was Added

### 1) Process-level OTel bootstrap
- Added `internal/otel/otel.go` with `Setup(ctx, serviceName)`.
- Configures:
  - OTLP gRPC trace exporter (`otlptracegrpc.New`).
  - resource `service.name` (overridable via `OTEL_SERVICE_NAME`).
  - tracer provider with batch exporter.
  - W3C TraceContext + Baggage propagators.
- `cmd/bot/main.go` now initializes OTel at startup and shuts it down on exit.

### 2) End-to-end context propagation
- Added `Ctx context.Context` to `worker.Task` and `worker.Result`.
- Worker submissions and periodic tasks now include context.
- Worker `Run` creates a task-level span per queued task.

### 3) Storage instrumentation and context-aware DB calls
- `internal/storage/service.go` methods were migrated to receive `context.Context`.
- `internal/storage/postgres.go` uses `db.WithContext(ctx)` on all operations.
- GORM OTel plugin is enabled via `db.Use(tracing.NewPlugin())`.

### 4) HTTP client instrumentation
- Trakt and TMDB clients now use `otelhttp.NewTransport(http.DefaultTransport)`.
- Request builders switched to `http.NewRequestWithContext`.
- Trakt/TMDB client method signatures now accept context and propagate it.

### 5) Telegram + Worker tracing
- Telegram update middleware creates spans for updates and callback queries.
- `telegram.submit_task` span wraps task enqueue.
- `telegram.send_result` span wraps outbound Telegram action dispatch.
- Worker task dispatch now creates spans named from task type (`worker.<task_type>`).

## How It Was Added

1. Introduced OTel setup package.
2. Extended task/result models with context.
3. Threaded context through storage, Trakt, and TMDB layers.
4. Added middleware and spans at Telegram ingress and egress boundaries.
5. Added worker-level spans around task processing.
6. Added GORM + HTTP transport instrumentation to emit DB and HTTP spans.

## Trace Continuity Strategy
- For normal request-driven flows: parent context is propagated through task queue into worker.
- For deferred/background flows: trace linkage is preserved while avoiding cancellation coupling by using detached contexts that keep span context and baggage.
- Helper: `internal/otel/context.go` (`DetachedContext`).
