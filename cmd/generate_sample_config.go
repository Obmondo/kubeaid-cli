package main

import (
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/urfave/cli/v2"
)

type TemplateValues struct {
	K8sVersion,
	AMI string
}

func generateSampleConfig(ctx *cli.Context) error {
	// Verify that file doesn't already exist.
	if _, err := os.Stat(constants.OutputPathGeneratedConfig); err == nil {
		log.Fatalf("Config file already exists at %s", constants.OutputPathGeneratedConfig)
	}

	// Verify that the (user specified if not default) Kubernetes version is supported.
	k8sVersion := ctx.String(constants.FlagNameK8sVersion)
	if !slices.Contains(constants.SupportedK8sVersions, k8sVersion) {
		log.Fatalf("Unsupported K8s version : %s", k8sVersion)
	}

	var (
		cloud        = ctx.String(constants.FlagNameCloud)
		templateName = fmt.Sprintf("templates/%s.sample.config.yaml.tmpl", cloud)

		templateValues = &TemplateValues{
			K8sVersion: ctx.String(constants.FlagNameK8sVersion),
		}
	)

	// Cloud specific actions.
	switch cloud {
	case constants.CloudProviderAWS:
		// By default, use Obmondo published ARM and Ubuntu based AMI for the given Kubernetes version.
		templateValues.AMI = constants.ObmondoPublishedAMIs[k8sVersion]

	case constants.CloudProviderAzure:
	case constants.CloudProviderHetzner:
		log.Fatalf("Support for Azure and Hetzner is coming soon....")

	default:
		log.Fatalf("Unknown type of cloud provider : %s", cloud)
	}

	// Execute the template.
	content := utils.ParseAndExecuteTemplate(&config.SampleConfigs, templateName, templateValues)

	// Write the template execution result to the sample config file.
	destinationFile, err := os.OpenFile(constants.OutputPathGeneratedConfig, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("Failed opening file at %s : %v", constants.OutputPathGeneratedConfig, err)
	}
	defer destinationFile.Close()
	destinationFile.Write(content)

	return nil
}
