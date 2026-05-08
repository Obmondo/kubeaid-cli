// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"

	goGit "github.com/go-git/go-git/v5"
)

// gpgAgentSigner implements go-git's Signer interface by shelling
// out to gpg's local agent. Hardware-backed keys (YubiKey GPG slot,
// Nitrokey, etc.) work transparently — gpg-agent handles the
// smartcard interaction via pcscd, the operator taps when prompted,
// signature comes back. kubeaid-cli itself doesn't need to know
// whether the key is in software or hardware.
type gpgAgentSigner struct {
	keyID string
}

// Sign satisfies goGit.Signer. go-git encodes the commit object
// without its signature, hands us the bytes via message, expects
// the armored detached signature back. We pipe straight to
// `gpg --detach-sign --armor --local-user <keyID>` which produces
// exactly that.
func (s *gpgAgentSigner) Sign(message io.Reader) ([]byte, error) {
	cmd := exec.Command("gpg",
		"--detach-sign", "--armor",
		"--local-user", s.keyID,
	)
	cmd.Stdin = message

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gpg signing failed: %w (gpg stderr: %s)",
			err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

// CommitSigner returns a go-git Signer suitable for
// CommitOptions.Signer when ALL of these hold:
//
//   - git config commit.gpgsign == "true" (operator opted in);
//   - git config user.signingkey is set;
//   - gpg is on PATH;
//   - a GPG smartcard is plugged in (`gpg-card status` succeeds).
//
// Returns nil otherwise — caller passes nil to leave the commit
// unsigned. Same shape as the YubiKey-touch hint elsewhere: hardware
// presence is the trigger, not just configuration. With no card we
// stay unsigned rather than silently fall back to a software key
// the operator may not have meant to use for kubeaid-cli's commits.
func CommitSigner(ctx context.Context) goGit.Signer {
	if gitConfigGlobal(ctx, "commit.gpgsign") != "true" {
		return nil
	}
	keyID := gitConfigGlobal(ctx, "user.signingkey")
	if keyID == "" {
		slog.DebugContext(ctx, "Skip GPG-signing: user.signingkey unset")
		return nil
	}
	if _, err := exec.LookPath("gpg"); err != nil {
		slog.WarnContext(ctx,
			"commit.gpgsign=true but gpg not in PATH; commits will be unsigned",
			slog.Any("err", err),
		)
		return nil
	}
	if !gpgCardPresent(ctx) {
		slog.DebugContext(ctx,
			"Skip GPG-signing: no GPG smartcard detected (gpg-card status failed)")
		return nil
	}
	return &gpgAgentSigner{keyID: keyID}
}

// gitConfigGlobal returns the value of git config <key> from the
// operator's global config, or "" when the key is unset / git
// errors. Shell-out (rather than reading ~/.gitconfig directly) so
// includeIf, conditional includes, and any other config indirection
// the operator's set up just works.
func gitConfigGlobal(ctx context.Context, key string) string {
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--get", key)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// gpgCardPresent reports whether `gpg-card status` succeeds — i.e.,
// gpg can talk to a smartcard. Doesn't verify that the configured
// signingkey actually lives on this card; the signing call itself
// fails loudly if it doesn't, which is the right place to surface
// that misconfiguration.
func gpgCardPresent(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "gpg-card", "status")
	// Suppress stdout/stderr — we only care about the exit code.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
