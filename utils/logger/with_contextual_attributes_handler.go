package logger

import (
	"context"
	"log/slog"
)

type SlogAttributesCtxKey struct{}

var slogAttributesCtxKey = SlogAttributesCtxKey{}

type ContextualSlogAttributesHandler struct {
	slog.Handler
}

// Retrieves slog attributes present in the context and appends them to the slog record (log event).
// The slog handler is then invoked with the modified record.
// We do this to print out the contextual slog attributes.
func (h *ContextualSlogAttributesHandler) Handle(ctx context.Context, record slog.Record) error {
	if attributes, ok := ctx.Value(slogAttributesCtxKey).([]slog.Attr); ok {
		for _, attribute := range attributes {
			record.AddAttrs(attribute)
		}
	}

	return h.Handler.Handle(ctx, record)
}

func withContextualSlogAttributesHandler(handler slog.Handler) *ContextualSlogAttributesHandler {
	return &ContextualSlogAttributesHandler{
		handler,
	}
}

// Appends slog attributes to the given context. The modified context is then returned.
func AppendSlogAttributesToCtx(ctx context.Context, attributes []slog.Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	existingSlogAttributes, ok := ctx.Value(slogAttributesCtxKey).([]slog.Attr)
	if !ok {
		existingSlogAttributes = []slog.Attr{}
	}
	existingSlogAttributes = append(existingSlogAttributes, attributes...)
	return context.WithValue(ctx, slogAttributesCtxKey, existingSlogAttributes)
}
