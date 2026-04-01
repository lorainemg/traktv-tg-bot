package otel

import (
	"context"

	"go.opentelemetry.io/otel/baggage"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// DetachedContext returns a non-cancelable context that keeps trace linkage.
// This is useful for deferred/background work that should survive request cancellation
// while still belonging to the same trace tree.
func DetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	detached := context.Background()
	if bg := baggage.FromContext(ctx); len(bg.Members()) > 0 {
		detached = baggage.ContextWithBaggage(detached, bg)
	}
	if spanCtx := oteltrace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		detached = oteltrace.ContextWithSpanContext(detached, spanCtx)
	}

	return detached
}
