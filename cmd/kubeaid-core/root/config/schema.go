// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config/schema"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

var SchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the general.yaml / secrets.yaml form schema as JSON",

	Long: `Prints a machine-readable description of every general.yaml and
secrets.yaml field — path, type, tier, required, default, enum and
appliesWhen — read-only, no config files or cluster required.

This is what the Obmondo API's add-cluster web form renders from: invoke
a pinned kubeaid-cli release binary and parse stdout as JSON.`,

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		spec, err := schema.FormSpec()
		assert.AssertErrNil(ctx, err, "Failed loading form spec")

		encoded, err := json.MarshalIndent(spec, "", "  ")
		assert.AssertErrNil(ctx, err, "Failed marshaling form spec")

		_, err = os.Stdout.Write(append(encoded, '\n'))
		assert.AssertErrNil(ctx, err, "Failed writing form spec to stdout")
	},
}
