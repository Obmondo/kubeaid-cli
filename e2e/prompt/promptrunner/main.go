// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// testcmd is a minimal binary that only runs the interactive config prompt.
// It is used by e2e tests to drive the prompt flow via a pseudo-terminal
// without triggering cluster bootstrap side effects.
package main

import (
	"fmt"
	"os"

	"github.com/Obmondo/kubeaid-cli/pkg/config/prompt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testcmd <configs-directory> [cluster-name]")
		os.Exit(1)
	}

	// Optional so existing single-argument invocations keep driving the
	// prompt with nothing pre-filled.
	var clusterName string
	if len(os.Args) > 2 {
		clusterName = os.Args[2]
	}

	if err := prompt.ConfigFromPrompt(os.Args[1], clusterName); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
