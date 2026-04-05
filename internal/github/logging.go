package github

import (
	"context"
	"log/slog"
)

type contextKey string

const correlationIDKey contextKey = "correlation_id"

// WithCorrelationID returns a context with the given correlation ID attached.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// CorrelationID extracts the correlation ID from the context, or returns empty string.
func CorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

// CorrelationHandler wraps a slog.Handler to automatically include correlation_id
// in all log records when present in the context.
type CorrelationHandler struct {
	slog.Handler
}

// NewCorrelationHandler wraps an existing handler with correlation ID injection.
func NewCorrelationHandler(h slog.Handler) *CorrelationHandler {
	return &CorrelationHandler{Handler: h}
}

// Handle adds the correlation_id attribute if present in the context.
func (h *CorrelationHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes.
func (h *CorrelationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CorrelationHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group.
func (h *CorrelationHandler) WithGroup(name string) slog.Handler {
	return &CorrelationHandler{Handler: h.Handler.WithGroup(name)}
}
