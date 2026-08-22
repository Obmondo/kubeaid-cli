// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// The end-to-end regression lives in cmd/kubeaid-cli/main_test.go, which drives
// the real binary. These cover the wiring that test cannot see: which commands
// the guard answers for, and that no command quietly falls outside it.

// A subcommand of bootstrap reaches bootstrap's hook too, so it inherits
// bootstrap's own preparation. Comparing one pointer instead of walking the
// chain would hand it back the ordering this guard exists to fix.
func TestSubcommandsOfBootstrapArePreparedByBootstrap(t *testing.T) {
	child := &cobra.Command{Use: "child"}
	BootstrapCmd.AddCommand(child)
	t.Cleanup(func() { BootstrapCmd.RemoveCommand(child) })

	assert.True(t, preparedByCommand(child))
	assert.True(t, preparedByCommand(BootstrapCmd))
	assert.False(t, preparedByCommand(ClusterCmd))
}

// Anything the guard does not answer for is prepared by ClusterCmd's hook, so
// it must not also prepare itself — that runs the whole parse, secret-filling
// and bare-metal SSH check twice. Walks the full subtree: `delete` already has
// grandchildren.
func TestCommandsOutsideTheGuardDoNotPrepareThemselves(t *testing.T) {
	var walk func(parent *cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if !preparedByCommand(cmd) {
				assert.Nil(t, cmd.PersistentPreRun,
					"%s prepares itself but is not covered by preparedByCommand, so it is prepared twice",
					cmd.CommandPath())
			}

			walk(cmd)
		}
	}

	walk(ClusterCmd)
}
