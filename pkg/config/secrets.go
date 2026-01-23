// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

type (
	SecretsConfig struct {
		ArgoCD  ArgoCDCredentials   `yaml:"argoCD"`
		AWS     *AWSCredentials     `yaml:"aws"`
		Azure   *AzureCredentials   `yaml:"azure"`
		Hetzner *HetznerCredentials `yaml:"hetzner"`
	}

	ArgoCDCredentials struct {
		// Git specific credentials, used by ArgoCD to watch the KubeAid and KubeAid Config repositories.
		//
		// NOTE : We enforce the user, not to make ArgoCD use SSH authentication against the Git server,
		//        since : that way, ArgoCD gets both read and write permissions.
		Git GitCredentials `yaml:"git"`
	}

	GitCredentials struct {
		Username string `yaml:"username" validate:"notblank"`
		Password string `yaml:"password" validate:"notblank"`
	}

	AWSCredentials struct {
		AWSAccessKeyID     string `yaml:"accessKeyID"     validate:"notblank"`
		AWSSecretAccessKey string `yaml:"secretAccessKey" validate:"notblank"`
		AWSSessionToken    string `yaml:"sessionToken"`
	}

	AzureCredentials struct {
		ClientID     string `yaml:"clientID"     validate:"notblank"`
		ClientSecret string `yaml:"clientSecret" validate:"notblank"`
	}

	HetznerCredentials struct {
		APIToken string                   `yaml:"apiToken" validate:"notblank"`
		Robot    *HetznerRobotCredentials `yaml:"robot"`
	}

	HetznerRobotCredentials struct {
		User     string `yaml:"user"     validate:"notblank"`
		Password string `yaml:"password" validate:"notblank"`
	}
)
