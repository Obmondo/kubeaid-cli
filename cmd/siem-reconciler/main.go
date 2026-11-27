// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Command siem-reconciler brings Keycloak, DFIR-IRIS, Wazuh and
// Velociraptor in line with the tenants.json rendered by the
// security-operations chart. See docs/siem-reconciler.md.
package main

import (
	"fmt"
	"os"

	"github.com/Obmondo/kubeaid-cli/cmd/siem-reconciler/root"
)

func main() {
	if err := root.RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
