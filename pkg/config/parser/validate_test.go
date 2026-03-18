// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

func TestValidateConfigsExitsWhenClusterNameContainsDots(t *testing.T) {
	ctx := context.Background()
	config.ParsedGeneralConfig = &config.GeneralConfig{
		Git: config.GitConfig{
			SSHUsername: "git",
		},
		Forks: config.ForksConfig{
			KubeaidFork: config.KubeAidForkConfig{
				URL:     "git@github.com:example/kubeaid.git",
				Version: "v1.0.0",
			},
			KubeaidConfigFork: config.KubeaidConfigForkConfig{
				URL: "git@github.com:example/kubeaid-config.git",
			},
		},
		Cluster: config.ClusterConfig{
			Name:       "kube.cluster.name",
			Type:       "workload",
			K8sVersion: "v1.31.0",
			ArgoCD: config.ArgoCDConfig{
				DeployKeys: config.DeployKeysConfig{
					KubeaidConfig: config.SSHPrivateKeyConfig{
						PrivateKeyFilePath: "kubeaid-config-key",
						PrivateKey:         "kubeaid-config-private-key",
					},
					Kubeaid: &config.SSHPrivateKeyConfig{
						PrivateKeyFilePath: "kubeaid-key",
						PrivateKey:         "kubeaid-private-key",
					},
				},
			},
		},
		Cloud: config.CloudConfig{
			Local: &config.LocalConfig{},
		},
	}
	config.ParsedSecretsConfig = &config.SecretsConfig{}

	err := validateConfigs(ctx)

	assert.EqualError(t, err, "cluster name cannot contain any dots")
}
