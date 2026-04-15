// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// testcmd is a minimal binary that only runs the interactive config prompt.
// It is used by e2e tests to drive the prompt flow via a pseudo-terminal
// without triggering cluster bootstrap side effects.
package main

import (
	"fmt"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/prompt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testcmd <configs-directory>")
		os.Exit(1)
	}
	if err := prompt.ConfigFromPrompt(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
