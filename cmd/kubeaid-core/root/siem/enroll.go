// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var enrollFlags struct {
	os        string
	outputDir string
}

// EnrollCmd is `siem enroll`.
var EnrollCmd = &cobra.Command{
	Use:   "enroll <tenant>",
	Short: "Print the agent install commands of a tenant",
	Long: `Reads the tenant's enrolment-bundle Secret (written by the reconciler in
wazuh-<tenant>) and prints the Wazuh agent install script for --os, with the
Velociraptor client hint. The script carries the tenant's enrolment password:
treat the output as a secret. --output-dir writes the script and the
Velociraptor client config there (mode 0600) instead of printing.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := siemctl.ParseEnrolOS(enrollFlags.os)
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		c, err := newClients(ctx)
		if err != nil {
			return err
		}
		bundle, err := siemctl.ReadEnrolmentBundle(ctx, c.kube, args[0])
		if err != nil {
			return err
		}
		if enrollFlags.outputDir != "" {
			written, err := bundle.WriteFiles(enrollFlags.outputDir, target)
			for _, p := range written {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", p)
			}
			return err
		}
		return bundle.WriteInstructions(cmd.OutOrStdout(), target)
	},
}

func init() {
	SiemCmd.AddCommand(EnrollCmd)
	EnrollCmd.Flags().StringVar(&enrollFlags.os, "os", "linux", "Agent operating system: linux, macos or windows")
	EnrollCmd.Flags().StringVar(&enrollFlags.outputDir, "output-dir", "", "Write the files there instead of printing")
}
