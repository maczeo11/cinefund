// Package logger configures slog and carries it on the context.
//
// Redaction is handled here rather than at call sites, because a call site that
// has to remember to redact is a call site that will eventually forget.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey struct{}

// sensitive keys are replaced wholesale. Matching is substring and
// case-insensitive so "razorpay_key_secret" and "Authorization" both hit.
var sensitive = []string{
	"password", "secret", "token", "authorization", "cookie",
	"signature", "card", "cvv", "api_key", "apikey",
}

func redact(groups []string, a slog.Attr) slog.Attr {
	_ = groups
	k := strings.ToLower(a.Key)
	for _, s := range sensitive {
		if strings.Contains(k, s) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}
	return a
}

// New builds the process logger. Development gets human-readable text; anything
// else gets JSON, because that is what a log aggregator can query.
func New(env, service, version string) *slog.Logger {
	level := slog.LevelInfo
	if env == "development" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: redact}

	var h slog.Handler
	if env == "development" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h).With(
		slog.String("service", service),
		slog.String("version", version),
	)
}

// Into stores a logger on the context.
func Into(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From retrieves the request-scoped logger, falling back to the default so a
// missing logger never panics in a handler.
func From(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
