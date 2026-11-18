// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package commandexecutor

import "context"

type CommandExecutor interface {
	// Executes shell commands.
	// Returns the stdout contents.
	Execute(ctx context.Context, command string) (string, error)
}
