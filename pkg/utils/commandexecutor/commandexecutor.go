// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package commandexecutor

import "context"

type CommandExecutor interface {
	// Executes shell commands.
	// Returns the stdout contents.
	Execute(ctx context.Context, command string) (string, error)

	// Executes shell commands.
	// Returns the stdout contents.
	// Panics, on failure.
	MustExecute(ctx context.Context, command string) string
}
