// Package termsetup prevents lipgloss/termenv from sending OSC escape
// sequences to query the terminal's background color. Those queries leak
// visible "rgb:…" strings when stdout flows through a Docker PTY.
//
// Blank-import this package in every main that transitively imports bubbletea.
//
// This package depends only on termenv, so Go initialises it before lipgloss
// (which depends on termenv + colorprofile + others). When lipgloss later
// calls termenv.DefaultOutput() to build its renderer, it picks up the
// modified output that never issues terminal queries. By the time bubbletea's
// init() calls lipgloss.HasDarkBackground(), the query is a no-op.
package termsetup

import (
	"io"
	"os"

	"github.com/muesli/termenv"
)

// noFdWriter wraps an io.Writer, hiding the Fd() method so termenv's TTY()
// returns nil and skips the OSC query, while WithTTY(true) keeps color
// detection working via environment variables.
type noFdWriter struct {
	io.Writer
}

func init() {
	out := termenv.NewOutput(
		&noFdWriter{os.Stdout},
		termenv.WithTTY(true),
		termenv.WithProfile(termenv.TrueColor),
	)
	termenv.SetDefaultOutput(out)
}
