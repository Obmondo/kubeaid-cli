// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

type (
	SecretsConfig struct {
		AWS      *AWSCredentials      `yaml:"aws"`
		Azure    *AzureCredentials    `yaml:"azure"`
		Hetzner  *HetznerCredentials  `yaml:"hetzner"`
		Obmondo  *ObmondoCredentials  `yaml:"obmondo"`
		Keycloak *KeycloakCredentials `yaml:"keycloak"`
	}

	// KeycloakCredentials carries the OIDC client secrets the
	// operator must hand kubeaid-cli when cluster.keycloak.mode is
	// "external". Managed-mode bootstrap reconciles these clients
	// itself via gocloak and persists the secrets in-cluster, so
	// this block stays empty in that case.
	KeycloakCredentials struct {
		// NetBirdBackendClientSecret is the confidential-client
		// secret the operator created for the netbird-backend
		// client in their external Keycloak realm. Templated into
		// the netbird Secret's idpClientMgmtSecret key.
		NetBirdBackendClientSecret string `yaml:"netBirdBackendClientSecret"`
	}

	ObmondoCredentials struct {
		// TeleportAuthToken is the join token teleport-kube-agent uses to
		// register with the Teleport cluster. Required when
		// obmondo.monitoring is true and obmondo.teleportAgent isn't false.
		TeleportAuthToken string `yaml:"teleportAuthToken"`
	}

	AWSCredentials struct {
		AWSAccessKeyID     string `yaml:"accessKeyID"     validate:"notblank"`
		AWSSecretAccessKey string `yaml:"secretAccessKey" validate:"notblank"`
		AWSSessionToken    string `yaml:"sessionToken"`
	}

	AzureCredentials struct {
		ClientID string `yaml:"clientID" validate:"notblank"`
		//nolint:gosec // This struct intentionally models user-provided Azure credentials.
		ClientSecret string `yaml:"clientSecret" validate:"notblank"`
	}

	HetznerCredentials struct {
		//nolint:gosec // This struct intentionally models user-provided Hetzner credentials.
		APIToken string                   `yaml:"apiToken" validate:"notblank"`
		Robot    *HetznerRobotCredentials `yaml:"robot"`
	}

	HetznerRobotCredentials struct {
		User string `yaml:"user" validate:"notblank"`
		//nolint:gosec // This struct intentionally models user-provided Hetzner Robot credentials.
		Password string `yaml:"password" validate:"notblank"`
	}
)
