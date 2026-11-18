// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-cli/pkg/cert"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/templates"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// getTemplateValues returns the values the cluster's KubeAid Config files render with: those the
// parsed config yields, plus what is looked up from the cloud, the main cluster and the
// file-system.
func getTemplateValues(ctx context.Context) *templates.TemplateValues {
	cfg := cliConfig()
	general := cfg.General

	inputs := templates.Inputs{
		GeneralConfigFileContents: config.GeneralConfigFileContents,
		CLIVersion:                globals.KubeaidCLIVersion,

		Azure: templates.AzureInputs{
			CAPIUAMIClientID:        globals.CAPIUAMIClientID,
			VeleroUAMIClientID:      globals.VeleroUAMIClientID,
			StorageAccountAccessKey: globals.AzureStorageAccountAccessKey,
		},
	}

	// Populate Hetzner bare-metal host public IPs via Robot API for the
	// kubelet-csr-approver values template. Empty map for non-Hetzner
	// or non-bare-metal setups (the values template guards on the map
	// being non-empty before rendering its per-host /32 entries).
	if globals.CloudProviderName == constants.CloudProviderHetzner {
		if hetznerProvider, ok := globals.CloudProvider.(*hetzner.Hetzner); ok {
			publicIPs, err := hetznerProvider.GetHetznerBareMetalHostPublicIPs(ctx)
			assert.AssertErrNil(ctx, err,
				"Failed fetching Hetzner bare-metal host public IPs from Robot API")
			inputs.HetznerBareMetalHostPublicIPs = publicIPs
		}
	}

	// Extract the Subject CN from the Obmondo mTLS cert when monitoring is on.
	// kube-prometheus's common-template fails hard if certname is missing.
	// Also load the cert + key file contents so the obmondo-clientcert
	// sealed-secret templates can base64-encode them. Paths are validated by
	// validateConfigs — re-read here to fail with context if they became
	// unreadable between parse and render.
	if general.ObmondoIntegrationEnabled() {
		obmondo := general.Obmondo

		cn, certErr := cert.ReadCN(obmondo.CertPath)
		assert.AssertErrNil(ctx, certErr, "Failed reading Obmondo cert CN",
			slog.String("path", obmondo.CertPath))

		certData, err := os.ReadFile(obmondo.CertPath)
		assert.AssertErrNil(ctx, err,
			"Failed reading Obmondo cert file",
			slog.String("path", obmondo.CertPath))

		key, err := os.ReadFile(obmondo.KeyPath)
		assert.AssertErrNil(ctx, err,
			"Failed reading Obmondo key file",
			slog.String("path", obmondo.KeyPath))

		inputs.ObmondoCert = &templates.ObmondoCert{
			CN:               cn,
			CertFileContents: string(certData),
			KeyFileContents:  string(key),
		}
	}

	if general.VPNClusterEnabled() {
		// postgresDSN is CNPG-generated and only available in-cluster
		// after the netbird-pgsql Cluster CR is reconciled. On the
		// first kubeaid-cli run the Secret doesn't have the key yet
		// → render an empty string; bootstrap_cluster.go's
		// netbird.WaitAndPatchPostgresDSN step fills it in post-sync. On
		// re-runs the patched value is read back here so the
		// SealedSecret in git stays in sync. This is the only field
		// that genuinely needs a cluster read — kubeaid-cli has no
		// way to know CNPG's randomly-generated password ahead of
		// the cluster being up.
		clusterClient, _ := kubernetes.CreateKubernetesClient(ctx,
			constants.OutputPathMainClusterKubeconfig,
		)
		inputs.NetBirdPostgresDSN = readSecretValueOrEmpty(ctx, clusterClient,
			constants.NamespaceNetBird,
			constants.SecretNameNetBird,
			constants.SecretKeyNetBirdPostgresDSN,
		)
	}

	// Set cloud provider specific values.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		accountID, accountErr := aws.GetAccountID(ctx)
		assert.AssertErrNil(ctx, accountErr, "Failed getting AWS account ID")

		inputs.AWS = templates.AWSInputs{
			AccountID:             accountID,
			B64EncodedCredentials: os.Getenv(constants.EnvNameAWSB64EcodedCredentials),
		}

	case constants.CloudProviderAzure:
		// AKS clusters have no operator-hosted OIDC provider — the issuer URL
		// derives from the storage account, which AKS configs don't carry.
		if !general.AKSEnabled() {
			saIssuerURL, saErr := azure.GetServiceAccountIssuerURL()
			assert.AssertErrNil(ctx, saErr, "Failed getting Azure ServiceAccount issuer URL")
			inputs.Azure.ServiceAccountIssuerURL = saIssuerURL
		}
	}

	// For local/dev clusters, the main cluster endpoint may not be available yet. Hetzner and
	// Bare Metal clusters have it in the config instead.
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner, constants.CloudProviderBareMetal:

	default:
		endpoint, endpointErr := kubeCluster().GetMainClusterEndpoint(ctx,
			constants.OutputPathMainClusterKubeconfig,
		)
		assert.AssertErrNil(ctx, endpointErr, "Failed getting main cluster endpoint")
		inputs.MainClusterEndpoint = endpoint
	}

	templateValues, err := templates.NewTemplateValues(cfg, inputs)
	assert.AssertErrNil(ctx, err, "Failed building the KubeAid Config template values")
	return templateValues
}

// embeddedNonSecretTemplateNames returns the names of the non Secret templates to render for the
// cluster the CLI parsed.
func embeddedNonSecretTemplateNames() []string {
	return templates.NonSecretTemplateNames(cliConfig())
}

// embeddedSecretTemplateNames returns the names of the Secret templates to render for the cluster
// the CLI parsed.
func embeddedSecretTemplateNames() []string {
	return templates.SecretTemplateNames(cliConfig())
}

// acmeDNS01Enabled reports whether the cluster's ClusterIssuer uses a DNS-01 solver.
func acmeDNS01Enabled() bool {
	return config.ParsedGeneralConfig.Cluster.ACMEDNS01 != nil
}
