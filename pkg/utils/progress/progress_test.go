// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoopBarIsSafe — every method on a zero-value Bar (the
// "no-op" returned by FromCtx for unattached contexts) must
// silently no-op. Guards library callers + unit tests that
// don't stand up a real progressbar.
func TestNoopBarIsSafe(t *testing.T) {
	var b *Bar // nil pointer
	require.NotPanics(t, func() {
		b.Describe("step")
		b.Substep("sub")
		release := b.RequestYubiKeyTouch()
		release()
		b.Finish()
	})

	zero := &Bar{} // non-nil but bar field is nil
	require.NotPanics(t, func() {
		zero.Describe("step")
		zero.Substep("sub")
		release := zero.RequestYubiKeyTouch()
		release()
		zero.Finish()
	})
}

func TestFromCtxRoundtrip(t *testing.T) {
	bar := New("header")
	defer bar.Finish()

	ctx := WithBar(context.Background(), bar)
	got := FromCtx(ctx)
	assert.Same(t, bar, got)
}

func TestFromCtxNoAttachReturnsNoop(t *testing.T) {
	got := FromCtx(context.Background())
	require.NotNil(t, got)
	require.Same(t, noopBar, got)

	require.NotPanics(t, func() {
		got.Substep("nothing happens")
	})
}

func TestSubstepTracksLastForTreeRedraw(t *testing.T) {
	bar := New("header")
	defer bar.Finish()
	bar.Describe("Provisioning")

	bar.Substep("Created Network")
	assert.Equal(t, "Created Network", bar.lastSubstep)

	bar.Substep("Created NAT Gateway")
	assert.Equal(t, "Created NAT Gateway", bar.lastSubstep)
}
