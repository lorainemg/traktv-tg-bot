package telegram

import (
	"net/http"
	pathpkg "path"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const telegramPollTimeout = time.Minute

func withInstrumentedHTTPClientOption() bot.Option {
	transport := newTelegramTracingTransport(http.DefaultTransport)
	client := &http.Client{
		Timeout:   telegramPollTimeout,
		Transport: transport,
	}
	return bot.WithHTTPClient(telegramPollTimeout, client)
}

func newTelegramTracingTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	otelTransport := otelhttp.NewTransport(base,
		otelhttp.WithFilter(func(r *http.Request) bool {
			return telegramAPIMethod(r.URL.Path) != "getUpdates"
		}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			method := telegramAPIMethod(r.URL.Path)
			if method == "" {
				return "telegram.api"
			}
			return "telegram.api." + method
		}),
	)

	return &telegramMethodAttributeTransport{base: otelTransport}
}

type telegramMethodAttributeTransport struct {
	base http.RoundTripper
}

func (t *telegramMethodAttributeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	method := telegramAPIMethod(req.URL.Path)
	if method != "" && method != "getUpdates" {
		span := trace.SpanFromContext(req.Context())
		if span.SpanContext().IsValid() {
			span.SetAttributes(attribute.String("telegram.api.method", method))
		}
	}

	return t.base.RoundTrip(req)
}

func telegramAPIMethod(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	method := pathpkg.Base(strings.TrimSuffix(trimmed, "/"))
	if method == "." || method == "/" {
		return ""
	}
	return method
}
