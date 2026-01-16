// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package generate

import (
	_ "embed"
	"log/slog"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

var (
	//go:embed general.yaml
	SampleConfigFileGeneral string

	//go:embed secrets.yaml
	SampleConfigFileSecrets string
)

var GenerateCmd = &cobra.Command{
	Use: "generate",

	Short: "Generate sample general.yaml and secrets.yaml config files for KubeAid CLI",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		// Verify that config files directory doesn't already exist.
		_, err := os.Stat(globals.ConfigsDirectory)
		if err == nil {
			slog.ErrorContext(cmd.Context(), "Config files directory already exists",
				slog.String("path", globals.ConfigsDirectory),
			)
			os.Exit(1)
		}

		// Create the configs directory.
		err = os.MkdirAll(globals.ConfigsDirectory, 0o750)
		assert.AssertErrNil(ctx, err,
			"Failed creating configs directory",
			slog.String("path", globals.ConfigsDirectory),
		)

		// Create the sample general.yaml file.
		{
			generalConfigFilePath := path.Join(globals.ConfigsDirectory, "general.yaml")

			sampleGeneralConfigFile, err := os.OpenFile(generalConfigFilePath,
				os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				0o600,
			)
			assert.AssertErrNil(ctx, err,
				"Failed opening file",
				slog.String("path", generalConfigFilePath),
			)
			defer sampleGeneralConfigFile.Close()

			_, err = sampleGeneralConfigFile.Write([]byte(SampleConfigFileGeneral))
			assert.AssertErrNil(ctx, err,
				"Failed writing sample config to file",
				slog.String("path", generalConfigFilePath),
			)
		}

		// Create the sample secrets.yaml file.
		{
			secretsConfigFilePath := path.Join(globals.ConfigsDirectory, "secrets.yaml")

			sampleSecretsConfigFile, err := os.OpenFile(secretsConfigFilePath,
				os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
				0o600,
			)
			assert.AssertErrNil(ctx, err,
				"Failed opening file",
				slog.String("path", secretsConfigFilePath),
			)
			defer sampleSecretsConfigFile.Close()

			_, err = sampleSecretsConfigFile.Write([]byte(SampleConfigFileSecrets))
			assert.AssertErrNil(ctx, err,
				"Failed writing sample config to file",
				slog.String("path", secretsConfigFilePath),
			)
		}
	},
}
