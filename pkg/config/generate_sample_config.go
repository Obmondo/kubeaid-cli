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
	var templateName string
	switch cloudProvider {
	case constants.CloudProviderAWS:
		templateName = constants.TemplateNameAWSSampleConfig

	case constants.CloudProviderAzure:
		panic("unimplemented")

	case constants.CloudProviderHetzner:
		templateName = constants.TemplateNameHetznerSampleConfig

	case constants.CloudProviderLocal:
		templateName = constants.TemplateNameLocalSampleConfig

	default:
		panic("unreachable")
	}

	content := templates.ParseAndExecuteTemplate(ctx, &SampleConfigs, templateName, nil)

	destinationFile, err := os.OpenFile(constants.OutputPathGeneratedConfig, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	assert.AssertErrNil(ctx, err,
		"Failed opening file",
		slog.String("path", constants.OutputPathGeneratedConfig),
	)
	defer destinationFile.Close()

	_, err = destinationFile.Write(content)
	assert.AssertErrNil(ctx, err,
		"Failed writing sample config to file",
		slog.String("path", constants.OutputPathGeneratedConfig),
	)
}
