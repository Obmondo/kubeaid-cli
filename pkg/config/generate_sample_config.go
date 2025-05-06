package config

import (
	"context"
	"embed"
	"log/slog"
	"os"
	"path"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

//go:embed files/templates/*
var SampleConfigs embed.FS

func GenerateSampleConfig(ctx context.Context, cloudProvider string) {
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

	{
		sampleGeneralConfigContent := templates.ParseAndExecuteTemplate(ctx,
			&SampleConfigs,
			generalTemplateName,
			nil,
		)

		sampleGeneralConfigFilePath := path.Join(
			constants.OutputPathGeneratedConfigsDirectory,
			generalTemplateName,
		)

		sampleConfigFile, err := os.OpenFile(
			sampleGeneralConfigFilePath,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0644,
		)
		assert.AssertErrNil(ctx, err,
			"Failed opening file",
			slog.String("path", sampleGeneralConfigFilePath),
		)
		defer sampleConfigFile.Close()

		_, err = sampleConfigFile.Write(sampleGeneralConfigContent)
		assert.AssertErrNil(ctx, err,
			"Failed writing sample config to file",
			slog.String("path", sampleGeneralConfigFilePath),
		)
	}

	{
		sampleSecretsConfigContent := templates.ParseAndExecuteTemplate(ctx,
			&SampleConfigs,
			secretsTemplateName,
			nil,
		)

		sampleSecretsConfigFilePath := path.Join(
			constants.OutputPathGeneratedConfigsDirectory,
			secretsTemplateName,
		)

		sampleConfigFile, err := os.OpenFile(
			sampleSecretsConfigFilePath,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0644,
		)
		assert.AssertErrNil(ctx, err,
			"Failed opening file",
			slog.String("path", sampleSecretsConfigFilePath),
		)
		defer sampleConfigFile.Close()

		_, err = sampleConfigFile.Write(sampleSecretsConfigContent)
		assert.AssertErrNil(ctx, err,
			"Failed writing sample config to file",
			slog.String("path", sampleSecretsConfigFilePath),
		)
	}
}
