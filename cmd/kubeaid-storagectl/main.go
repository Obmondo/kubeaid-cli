// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-storagectl/root"
)

func main() {
	err := root.RootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
