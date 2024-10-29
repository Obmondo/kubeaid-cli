package main

import (
	"log"
	"os"

	"github.com/guilherme-santos03/kubeaid-bootstrap-script-guilherme/cmd/bootstrap_cluster"
	"github.com/guilherme-santos03/kubeaid-bootstrap-script-guilherme/constants"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "KubeAid Bootstrap Script",
		Usage: "Bootstrap a Kubernetes cluster using KubeAid and ClusterAPI",
		Commands: []*cli.Command{
			{
				Name:   "generate-sample-config",
				Usage:  "Generate a sample configuration file",
				Action: generateSampleConfig,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     constants.FlagNameK8sVersion,
						Usage:    "Kubernetes version (v1.31.0 / v1.30.0)",
						Required: false,
						Value:    "v1.31.0",
					},
					&cli.StringFlag{
						Name:     constants.FlagNameCloud,
						Usage:    "Cloud provider (AWS / Azure / Hetzner)",
						Required: true,
					},
				},
			},
			{
				Name:   "bootstrap-cluster",
				Usage:  "Bootstrap a Kubernetes cluster and install KubeAid",
				Action: bootstrap_cluster.BootstrapCluster,
				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:     constants.FlagNameConfigFile,
						Usage:    "Path to the config file",
						Required: true,
					},
				},
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatalf("Failed running KubeAid Bootstrap script : %v", err)
	}
}
