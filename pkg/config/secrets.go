// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

type (
	SecretsConfig struct {
		Git     GitCredentials      `yaml:"git"`
		AWS     *AWSCredentials     `yaml:"aws"`
		Azure   *AzureCredentials   `yaml:"azure"`
		Hetzner *HetznerCredentials `yaml:"hetzner"`
	}

	GitCredentials struct {
		*GitUsernameAndPassword `yaml:",inline"`
	}

	GitUsernameAndPassword struct {
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
