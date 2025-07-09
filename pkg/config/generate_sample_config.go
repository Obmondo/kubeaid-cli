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

//go:embed templates/*
var SampleConfigs embed.FS

type GenerateSampleConfigArgs struct {
	CloudProvider string

	HetznerMode *string
}

func GenerateSampleConfig(ctx context.Context, args *GenerateSampleConfigArgs) {
	// Create configs directory.
	err := os.MkdirAll(constants.OutputPathGeneratedConfigsDirectory, os.ModePerm)
	assert.AssertErrNil(
		ctx,
		err,
		"Failed creating directory",
		slog.String("path", constants.OutputPathGeneratedConfigsDirectory),
	)

	// Based on the target cloud provider, determine templates to be used.
	// We'll generate the sample general and secrets config from those templates.
	var generalTemplateName,
		secretsTemplateName string
	switch args.CloudProvider {
	case constants.CloudProviderAWS:
		generalTemplateName = constants.TemplateNameAWSGeneralConfig
		secretsTemplateName = constants.TemplateNameAWSSecretsConfig

	case constants.CloudProviderAzure:
		generalTemplateName = constants.TemplateNameAzureGeneralConfig
		secretsTemplateName = constants.TemplateNameAzureSecretsConfig

	case constants.CloudProviderHetzner:
		switch *args.HetznerMode {
		case constants.HetznerModeHCloud:
			generalTemplateName = constants.TemplateNameHetznerHCloudGeneralConfig
			secretsTemplateName = constants.TemplateNameHetznerHCloudSecretsConfig

		case constants.HetznerModeBareMetal:
			generalTemplateName = constants.TemplateNameHetznerBareMetalGeneralConfig
			secretsTemplateName = constants.TemplateNameHetznerBareMetalSecretsConfig

		case constants.HetznerModeHybrid:
			generalTemplateName = constants.TemplateNameHetznerHybridGeneralConfig
			secretsTemplateName = constants.TemplateNameHetznerHybridSecretsConfig
		}

	case constants.CloudProviderBareMetal:
		generalTemplateName = constants.TemplateNameBareMetalGeneralConfig
		secretsTemplateName = constants.TemplateNameBareMetalSecretsConfig

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
