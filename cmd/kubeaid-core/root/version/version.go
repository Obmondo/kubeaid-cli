// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package version

import (
	"fmt"

	"github.com/spf13/cobra"
)

// These variables are set at build time via -ldflags.
// When not set (e.g. during development with `go run`), they fall back to defaults.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

var VersionCommand = &cobra.Command{
	Use: "version",

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("version: %s\ncommit:  %s\nbuilt:   %s\n", Version, Commit, Date)
	},
}
