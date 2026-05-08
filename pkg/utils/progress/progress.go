// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// pausableWriter wraps an io.Writer with a runtime mute switch. While
// muted, Write calls report success but discard the bytes — used to
// silence the progressbar's auto-render goroutine during interactive
// prompts so the spinner doesn't overwrite the prompt line.
type pausableWriter struct {
	inner  io.Writer
	paused atomic.Bool
}

func (w *pausableWriter) Write(p []byte) (int, error) {
	if w.paused.Load() {
		return len(p), nil
	}
	return w.inner.Write(p)
}

const (
	yubikeyTouchActivePrefix = "👉 Tap YubiKey to "

	// majorStepGlyph is the section marker for a major-step header.
	// Filled circle reads as "section bullet" rather than "completed
	// outcome" — the major step is the container, individual ✓
	// substeps inside are the outcomes.
	majorStepGlyph = "● "

	// substepIndent is what each substep line is prefixed with: two
	// spaces so the substeps sit visually under the major-step
	// header, then a "✓ " glyph so completed substeps read as work
	// items checked off.
	substepIndent = "  "
	substepGlyph  = "✓ "
)

// completedSubstepStyle dims completed substep rows so the operator's
// eye lands on the spinner / next active block, not on the audit-trail
// list of finished work. Bold/colored is reserved for active surfaces.
var completedSubstepStyle = lipgloss.NewStyle().Faint(true)

// majorStepHeaderStyle bolds the "● <step>" header so each section
// transition reads loud and clear in the transcript.
var majorStepHeaderStyle = lipgloss.NewStyle().Bold(true)

// Bar is a section-style progress logger. The transcript reads as:
//
//	● Provisioning Hetzner infrastructure
//	─────────────────────────────────────
//	  ✓ Created Hetzner Network
//	  ✓ Registered HCloud SSH key
//	  ✓ Created control-plane Load Balancer
//	⠴   [12s]
//
// Each major step opens a section: bold "● <step>" header on its own
// line, then a horizontal rule the same width as the header. Substeps
// stream below in faint "  ✓ <work>" rows as they complete. The bar's
// spinner ticks on the line below the section to telegraph "still in
// the middle of this section".
//
// A zero-value Bar{} (returned by FromCtx for contexts with no bar
// attached) is silently a no-op — every method nil-guards so test
// code and library callers don't have to care.
type Bar struct {
	bar         *progressbar.ProgressBar
	writer      *pausableWriter
	currentDesc string
	lastSubstep string

	// hasYubiKey is the cached "is a YubiKey-backed identity loaded
	// in the SSH agent" check, run once at New(). Gates
	// RequestYubiKeyTouch so a software-key-only agent (or no card
	// plugged in) doesn't trigger spurious touch sub-steps.
	hasYubiKey bool
}

// New creates a spinner-style progress bar (unknown length) with
// the given header. The header is the whole-run label; it does NOT
// get a "✓" line of its own — the first Describe call starts the
// first real step.
func New(description string) *Bar {
	pw := &pausableWriter{inner: os.Stderr}
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(pw),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionClearOnFinish(),
	)
	return &Bar{
		bar:        bar,
		writer:     pw,
		hasYubiKey: detectYubiKeyInAgent(),
	}
}

// Pause silences the bar's writes to stderr — including the spinner's
// 100ms auto-render goroutine inside progressbar/v3. Use around
// interactive stdin prompts so the spinner can't overwrite the prompt
// line via its `\r`-anchored re-render. Internal state (counters,
// elapsed time) keeps updating; only the visible output is suppressed.
//
// Pair with Resume. Calling Pause clears the spinner line via a direct
// stderr write so the prompt has a clean line to print into.
func (b *Bar) Pause() {
	if b == nil || b.writer == nil {
		return
	}
	b.writer.paused.Store(true)
	// Bar's own Clear would now be muted by the paused writer. Write
	// the clear-line + CR escape directly to the un-muted underlying
	// stderr so the spinner row is visibly cleared before the prompt
	// prints into it.
	fmt.Fprint(os.Stderr, "\033[2K\r")
}

// Resume re-enables the bar's writes. The spinner re-appears on the
// next render (within ~100ms via the auto-tick goroutine, or sooner
// if a Substep/Describe call triggers a render).
func (b *Bar) Resume() {
	if b == nil || b.writer == nil {
		return
	}
	b.writer.paused.Store(false)
}

// detectYubiKeyInAgent dials $SSH_AUTH_SOCK and returns true if any
// loaded identity has "cardno:" in its comment — the standard
// OpenSSH-agent / scdaemon marker for a smartcard-backed key
// (which the operator must touch when signing). False when there's
// no agent, no identities, or only software-backed identities.
//
// Run once at Bar construction; the result is cached and re-used
// by every RequestYubiKeyTouch call to avoid hammering the agent.
func detectYubiKeyInAgent() bool {
	sock := os.Getenv(constants.EnvNameSSHAuthSock)
	if sock == "" {
		return false
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false
	}
	defer conn.Close()
	identities, err := agent.NewClient(conn).List()
	if err != nil {
		return false
	}
	for _, k := range identities {
		if strings.Contains(k.Comment, "cardno:") {
			return true
		}
	}
	return false
}

// Describe opens a new major-step section. Prints a blank line (when
// transitioning from a previous section), the bold "● <step>" header,
// and a horizontal rule whose width matches the header text. Substeps
// that follow stream below in faint "  ✓ <work>" rows.
//
// Top-to-bottom the final transcript reads as:
//
//	● Provisioning Hetzner infrastructure
//	─────────────────────────────────────
//	  ✓ Created Hetzner Network
//	  ✓ Created NAT Gateway
//
//	● Creating management cluster
//	──────────────────────────────
//	  ✓ Cloned kubeaid-config repo
//	  ⠴   [12s]
//
// No-op for repeat Describe calls with the same description.
func (b *Bar) Describe(description string) {
	if b == nil || b.bar == nil {
		return
	}
	if description == b.currentDesc {
		return
	}

	_ = b.bar.Clear()
	if b.currentDesc != "" {
		// Blank line between sections; the underline is the section's
		// own opener so we don't need a closing rule on the previous.
		fmt.Fprintln(os.Stderr)
	}
	b.currentDesc = description
	b.lastSubstep = ""

	header := majorStepGlyph + description
	fmt.Fprintln(os.Stderr, majorStepHeaderStyle.Render(header))
	fmt.Fprintln(os.Stderr, strings.Repeat("─", utf8.RuneCountInString(header)))

	b.refreshCaption()
	_ = b.bar.Add(1)
}

// Substep prints "  ✓ <text>" in faint style under the active major-
// step header. Substeps stream below the section's underline as work
// completes; the bar's spinner re-renders below them on its next tick.
//
// Faint styling is applied immediately rather than retroactively
// (each substep is the audit trail of finished work — there's no
// "active substep" concept; the bar's spinner is the active surface).
// Operator's eye scans past the dim list and lands on the spinner.
func (b *Bar) Substep(text string) {
	if b == nil || b.bar == nil {
		return
	}
	_ = b.bar.Clear()
	fmt.Fprintln(os.Stderr,
		completedSubstepStyle.Render(substepIndent+substepGlyph+text),
	)
	b.lastSubstep = text
}

// RequestYubiKeyTouch emits a transient "👉 Tap YubiKey to <reason>"
// sub-step while the spinner is paused on a hardware-touch SSH
// signature, then ERASES that line once the SSH op completes —
// the work sub-step that follows ("Cloned X", "Pushed Y") is the
// audit trail. We don't keep a permanent "Touched ✓" line because
// a single major step often does several SSH ops back-to-back; a
// stack of identical "Touched ✓" lines would crowd out the real
// progress without adding info.
//
// reason names what the operator is authorizing — "clone repo",
// "push branch", "fetch updates" — and shows up in the prompt so
// they know what they're approving. Keep it short (a few words);
// the prompt is one substep line and shouldn't wrap.
//
// Pair via `defer bar.RequestYubiKeyTouch("...")()` around the actual
// SSH op. Caveat: the erase assumes no other Substep calls fire
// between Request and the closure call, so bracket as tightly as
// possible around the op — emitting other sub-steps in between
// will cause the erase to target the wrong line.
//
// No-op (returns a no-op closure) when no YubiKey-backed identity
// is loaded in the SSH agent — software-only agents never block
// for a hardware touch. Card detection is cached at Bar
// construction; plugging in the YubiKey mid-bootstrap won't be
// picked up until next run.
func (b *Bar) RequestYubiKeyTouch(reason string) (release func()) {
	if b == nil || b.bar == nil || !b.hasYubiKey {
		return func() {}
	}
	prevSubstep := b.lastSubstep

	_ = b.bar.Clear()
	// Print the touch row directly (NOT through Substep) so it doesn't
	// get the completed-✓ glyph + faint styling. This is an active
	// prompt, not finished work — it should look distinct.
	fmt.Fprintln(os.Stderr, substepIndent+yubikeyTouchActivePrefix+reason)
	b.lastSubstep = yubikeyTouchActivePrefix + reason

	return func() {
		_ = b.bar.Clear()
		// Cursor is at col 0 of the cleared spinner line. Move up
		// to the "Tap..." row, blank it; the next bar render lands
		// the spinner at the now-empty position so the touch leaves
		// no permanent trace. Restore the pre-touch lastSubstep so
		// any state machinery that consults it sees the right value.
		fmt.Fprint(os.Stderr, "\033[F\033[2K\r")
		b.lastSubstep = prevSubstep
	}
}

// Finish clears the spinner. Substeps are already a flat list under
// the section header so there's no tree branch to close — the next
// major-step section's header serves as the implicit boundary.
func (b *Bar) Finish() {
	if b == nil || b.bar == nil {
		return
	}
	b.currentDesc = ""
	b.lastSubstep = ""
	_ = b.bar.Finish()
}

// refreshCaption clears the spinner caption. The major-step header
// ("● <step>") already names the current section at the top of the
// visible block, and substeps fill the middle — repeating the
// major-step name on the spinner row is redundant noise. Spinner
// shows only its glyph + elapsed-time counter now.
func (b *Bar) refreshCaption() {
	b.bar.Describe("")
}
