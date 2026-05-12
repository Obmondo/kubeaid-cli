// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// RenderedLineCount returns the number of terminal rows the rendered
// block currently occupies, accounting for line wrapping at the
// detected terminal width. Use it as the argument to "\033[<N>F" (cursor
// up + clear) so the wipe lands exactly on the start of what was
// printed last time, even after the operator has resized (e.g. tmux
// split, font-size change, full-window resize) mid-render.
//
// `strings.Count(block, "\n")` alone is wrong on resize: the count is
// the number of logical newlines in the printed string, but the
// terminal may have wrapped each long line to 2+ rows. Cursor-up by
// the logical count then lands inside the previous block — the next
// render appends below it, accumulating ghost headers each tick
// (visible signature: empty "Resource | Phase | Status" rows stacking
// under a footer that keeps moving).
//
// Width detection falls back to logical-newline counting when stdout
// isn't a terminal or term.GetSize fails — same behaviour as before
// in non-interactive contexts (CI logs, redirected output).
func RenderedLineCount(block string) int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return strings.Count(block, "\n")
	}

	lines := strings.Split(block, "\n")
	// Strip the trailing empty element when block ends with "\n" so
	// the count reflects rows printed, not split fragments.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	rows := 0
	for _, line := range lines {
		visible := lipgloss.Width(line)
		if visible == 0 {
			rows++
			continue
		}
		// Ceiling division — a line whose visible width exceeds the
		// terminal width wraps to ceil(visible/width) rows.
		rows += (visible + width - 1) / width
	}
	return rows
}
