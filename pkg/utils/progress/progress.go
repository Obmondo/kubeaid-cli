// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package progress

import (
	"os"

	"github.com/schollz/progressbar/v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// Bar wraps a progressbar for use across bootstrap stages.
type Bar struct {
	bar *progressbar.ProgressBar
}

// New creates a spinner-style progress bar (unknown length) with the given initial description.
func New(description string) *Bar {
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionClearOnFinish(),
	)
	return &Bar{bar: bar}
}

// Describe updates the progress bar description to reflect the current stage.
func (b *Bar) Describe(description string) {
	b.bar.Describe(description)
	_ = b.bar.Add(1)
}

// DescribeWithYubiKeyHint is Describe with a "👉 touch YubiKey"
// suffix appended when an SSH agent socket is present in the
// environment. Use for steps that may block on a hardware-backed
// SSH signature so the operator knows the spinner is waiting on
// them. When SSH_AUTH_SOCK is unset (no agent in play) the suffix
// is dropped and behavior matches plain Describe.
func (b *Bar) DescribeWithYubiKeyHint(description string) {
	if os.Getenv(constants.EnvNameSSHAuthSock) != "" {
		description += " 👉 touch YubiKey"
	}
	b.Describe(description)
}

// Finish completes the progress bar.
func (b *Bar) Finish() {
	_ = b.bar.Finish()
}
