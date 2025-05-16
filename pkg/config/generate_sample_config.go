package config

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

//go:embed files/templates/*
var SampleConfigs embed.FS

func GenerateSampleConfig(ctx context.Context, cloudProvider string) {
	// Create configs directory.
	os.MkdirAll(constants.OutputPathGeneratedConfigsDirectory, os.ModePerm)

	// Based on the target cloud provider, determine templates to be used.
	// We'll generate the sample general and secrets config from those templates.
	var generalTemplateName,
		secretsTemplateName string
	switch cloudProvider {
	case constants.CloudProviderAWS:
		generalTemplateName = constants.TemplateNameAWSGeneralConfig
		secretsTemplateName = constants.TemplateNameAWSSecretsConfig

	case constants.CloudProviderAzure:
		generalTemplateName = constants.TemplateNameAzureGeneralConfig
		secretsTemplateName = constants.TemplateNameAzureSecretsConfig

	case constants.CloudProviderHetzner:
		generalTemplateName = constants.TemplateNameHetznerGeneralConfig
		secretsTemplateName = constants.TemplateNameHetznerSecretsConfig

	case constants.CloudProviderLocal:
		generalTemplateName = constants.TemplateNameLocalGeneralConfig
		secretsTemplateName = constants.TemplateNameLocalSecretsConfig

	default:
		panic("unreachable")
	}

	// Generate sample general config file.
	{
		sampleGeneralConfigContent := templates.ParseAndExecuteTemplate(ctx,
			&SampleConfigs,
			generalTemplateName,
			nil,
		)

		sampleGeneralConfigFile, err := os.OpenFile(
			constants.OutputPathGeneratedGeneralConfigFile,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0644,
		)
		assert.AssertErrNil(ctx, err,
			"Failed opening file",
			slog.String("path", constants.OutputPathGeneratedGeneralConfigFile),
		)
		defer sampleGeneralConfigFile.Close()

		_, err = sampleGeneralConfigFile.Write(sampleGeneralConfigContent)
		assert.AssertErrNil(ctx, err,
			"Failed writing sample config to file",
			slog.String("path", constants.OutputPathGeneratedGeneralConfigFile),
		)
	}

	// Generate sample secrets config file.
	{
		sampleSecretsConfigContent := templates.ParseAndExecuteTemplate(ctx,
			&SampleConfigs,
			secretsTemplateName,
			nil,
		)

		sampleSecretsConfigFile, err := os.OpenFile(
			constants.OutputPathGeneratedSecretsConfigFile,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0644,
		)
		assert.AssertErrNil(ctx, err,
			"Failed opening file",
			slog.String("path", constants.OutputPathGeneratedSecretsConfigFile),
		)
		defer sampleSecretsConfigFile.Close()

		_, err = sampleSecretsConfigFile.Write(sampleSecretsConfigContent)
		assert.AssertErrNil(ctx, err,
			"Failed writing sample config to file",
			slog.String("path", constants.OutputPathGeneratedSecretsConfigFile),
		)
	}
}
