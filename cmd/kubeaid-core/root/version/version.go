// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package version

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed version.txt
var Version string

var VersionCommand = &cobra.Command{
	Use: "version",

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("v" + Version)
	},
}
