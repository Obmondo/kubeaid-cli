// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import "context"

type ctxKey struct{}

// noopBar is returned by FromCtx when no bar is attached to the
// context — its bar field is nil, so every method short-circuits.
// Singleton pointer; cheap to dispense.
var noopBar = &Bar{}

// WithBar returns ctx carrying b. Helpers down the call tree can
// then retrieve the bar via FromCtx without threading *Bar through
// every signature.
func WithBar(ctx context.Context, b *Bar) context.Context {
	return context.WithValue(ctx, ctxKey{}, b)
}

// FromCtx returns the *Bar attached to ctx via WithBar, or a no-op
// Bar when none is attached. Callers can therefore always do
// `progress.FromCtx(ctx).Substep(...)` without nil-checking.
func FromCtx(ctx context.Context) *Bar {
	if b, ok := ctx.Value(ctxKey{}).(*Bar); ok && b != nil {
		return b
	}
	return noopBar
}
