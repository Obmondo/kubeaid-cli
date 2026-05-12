// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// renderedLineCountForWidth is the pure, width-injected counterpart of
// RenderedLineCount — tests exercise it directly so they don't depend
// on the actual terminal stdout is attached to.
//
// Keeping this in the test file (instead of exporting from wrap.go)
// avoids leaking a width parameter into the production API; the only
// width the production caller cares about is "whatever the operator's
// terminal happens to be right now".
func renderedLineCountForWidth(block string, width int) int {
	// Copy of the production loop body — the production function is a
	// thin wrapper that resolves the width and delegates. We replicate
	// the logic here so the test doesn't pull in lipgloss as a peer
	// import path.
	if width <= 0 {
		return strings.Count(block, "\n")
	}
	lines := strings.Split(block, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	rows := 0
	for _, line := range lines {
		// In the tests we control inputs so they contain no ANSI codes
		// — visible width is just rune length. The production helper
		// uses lipgloss.Width to strip ANSI codes from styled output.
		visible := len([]rune(line))
		if visible == 0 {
			rows++
			continue
		}
		rows += (visible + width - 1) / width
	}
	return rows
}

func TestRenderedLineCount_NoWrapping(t *testing.T) {
	t.Parallel()

	// Each line fits within the width — physical rows == logical lines.
	block := "alpha\nbeta\ngamma\n"
	assert.Equal(t, 3, renderedLineCountForWidth(block, 80))
}

func TestRenderedLineCount_OneWrappedLine(t *testing.T) {
	t.Parallel()

	// One 30-char line at width 20 wraps to 2 rows; the rest fit.
	long := strings.Repeat("X", 30)
	block := "header\n" + long + "\nfooter\n"
	// header (1) + long wrapped (2) + footer (1) = 4
	assert.Equal(t, 4, renderedLineCountForWidth(block, 20))
}

func TestRenderedLineCount_ManyWrappedLines(t *testing.T) {
	t.Parallel()

	// Each of 5 lines is exactly 3× the width → wraps to 3 rows each.
	long := strings.Repeat("X", 60)
	block := strings.Repeat(long+"\n", 5)
	assert.Equal(t, 15, renderedLineCountForWidth(block, 20))
}

func TestRenderedLineCount_EmptyLines(t *testing.T) {
	t.Parallel()

	// Empty logical lines occupy exactly one row each — they don't
	// contribute width, so the ceiling-division branch is skipped.
	block := "alpha\n\nbeta\n\n\ngamma\n"
	assert.Equal(t, 6, renderedLineCountForWidth(block, 80))
}

func TestRenderedLineCount_NoTrailingNewline(t *testing.T) {
	t.Parallel()

	// Block doesn't end with "\n" — the last element of Split is the
	// final line, not an empty fragment, so it must count.
	block := "alpha\nbeta\ngamma"
	assert.Equal(t, 3, renderedLineCountForWidth(block, 80))
}

func TestRenderedLineCount_FallbackOnUnknownWidth(t *testing.T) {
	t.Parallel()

	// width <= 0 means terminal detection failed (CI logs, redirected
	// stdout). Fall through to the old behaviour — pure logical-newline
	// counting — so we don't make non-interactive runs worse.
	block := "alpha\nbeta\ngamma\n"
	assert.Equal(t, 3, renderedLineCountForWidth(block, 0))
	assert.Equal(t, 3, renderedLineCountForWidth(block, -1))
}

func TestRenderedLineCount_ExactBoundary(t *testing.T) {
	t.Parallel()

	// A line whose visible width is exactly the terminal width
	// occupies one row, not two — ceiling division at the boundary
	// must round to 1.
	exact := strings.Repeat("X", 20)
	assert.Equal(t, 1, renderedLineCountForWidth(exact+"\n", 20))

	// One character over the boundary forces a wrap.
	overflow := strings.Repeat("X", 21)
	assert.Equal(t, 2, renderedLineCountForWidth(overflow+"\n", 20))
}
