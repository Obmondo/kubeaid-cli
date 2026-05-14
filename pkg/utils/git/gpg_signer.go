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
	cmd := exec.Command("gpg", //nolint:gosec // G204: keyID is the operator's own git config user.signingkey.
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
//   - git config gpg.format is empty or "openpgp" (this signer
//     doesn't handle ssh-format signatures);
//   - gpg is on PATH;
//   - a GPG smartcard is plugged in (`gpg --card-status` succeeds).
//
// Returns nil otherwise — caller passes nil to leave the commit
// unsigned. With no card we stay unsigned rather than silently fall
// back to a software key the operator may not have meant to use for
// kubeaid-cli's commits.
//
// When commit.gpgsign IS true but a downstream gate fails, we log
// at Info level so the operator sees why the commit went through
// unsigned — debug-only logs hid these "I expected signing, why
// didn't it sign" cases under the default log level.
func CommitSigner(ctx context.Context) goGit.Signer {
	if gitConfigGlobal(ctx, "commit.gpgsign") != "true" {
		return nil
	}

	// From here on, the operator has opted into signing — log every
	// gate failure so they can fix their config without sifting
	// through debug output.
	if format := gitConfigGlobal(ctx, "gpg.format"); format != "" && format != "openpgp" {
		slog.InfoContext(ctx,
			"commit.gpgsign=true but gpg.format is non-openpgp; this signer only handles openpgp — commits will be unsigned",
			slog.String("gpg.format", format),
		)
		return nil
	}
	keyID := gitConfigGlobal(ctx, "user.signingkey")
	if keyID == "" {
		slog.InfoContext(ctx,
			"commit.gpgsign=true but user.signingkey unset; commits will be unsigned")
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
		slog.InfoContext(ctx,
			"commit.gpgsign=true but no GPG smartcard detected (gpg --card-status failed); commits will be unsigned")
		return nil
	}

	slog.InfoContext(ctx, "GPG-signing kubeaid-cli commits",
		slog.String("user.signingkey", keyID),
	)
	return &gpgAgentSigner{keyID: keyID}
}

// gitConfigGlobal returns the value of git config <key> from the
// operator's global config, or "" when the key is unset / git
// errors. Shell-out (rather than reading ~/.gitconfig directly) so
// includeIf, conditional includes, and any other config indirection
// the operator's set up just works.
func gitConfigGlobal(ctx context.Context, key string) string {
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--get", key) //nolint:gosec // G204: key is always a compile-time-constant git-config key name.
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

// gpgCardPresent reports whether `gpg --card-status` succeeds —
// i.e., gpg can talk to a smartcard. We use the gpg subcommand
// rather than the standalone `gpg-card` binary because the latter
// only ships with GnuPG 2.3+; the subcommand has been around since
// 2.0 and Just Works on the systems kubeaid-cli targets.
//
// Doesn't verify that the configured signingkey actually lives on
// this card; the signing call itself fails loudly if it doesn't,
// which is the right place to surface that misconfiguration.
func gpgCardPresent(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "gpg", "--card-status")
	// Suppress stdout/stderr — we only care about the exit code.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
