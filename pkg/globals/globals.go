// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package globals

import (
	"io"
	"path"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

var (
	ConfigsDirectory,

	// ClusterName is --cluster-name: an alternative to spelling out
	// --configs-directory, resolved against the per-cluster convention in
	// pkg/config/clusterdir. Empty when the flag was not given.
	ClusterName,

	// KubeaidCLIVersion is the version of the kubeaid-cli binary, injected
	// from the version package via ldflags at build time. Set once at startup
	// in cmd/kubeaid-core/root/root.go. Empty string and "dev" both mean a
	// local / unreleased build; consumers must treat those the same way.
	KubeaidCLIVersion,

	CloudProviderName string
	CloudProvider cloud.CloudProvider

	// Config is the explicit config of the cluster the CLI parsed, which the CLI hands to the
	// library packages. Set by the config parser.
	Config *config.Config

	// Cluster runs the Kubernetes-side provisioning steps for Config, and owns the ArgoCD
	// application client they share. Set by the config parser.
	Cluster *kubernetes.Cluster

	// The control-plane endpoint hostname of the pre-provisioned HCloud control-plane LB (VPN
	// clusters), and the LB's public IP that the hostname resolves to during bootstrap. Mirrored
	// from Config.Provisioned after prerequisite infrastructure is provisioned, for the K3d
	// management cluster's host aliases.
	ControlPlaneHostname            string
	ControlPlaneLBBootstrapPublicIP string

	// Azure specific.
	CAPIUAMIClientID,
	VeleroUAMIClientID,
	AzureStorageAccountAccessKey string
	IsDebugModeEnabled bool

	// LogFile is this run's log file under outputs/logs/, opened once in
	// cmd/kubeaid-core/root/root.go. Writers other than the slog logger (e.g. captured KubeOne
	// output) must reuse this handle - the file isn't opened in append mode, so a second file
	// descriptor on the same path would overwrite it.
	LogFile     io.Writer
	LogFilePath string
)

// GeneralConfigFilePath is where general.yaml lives for this run.
//
// Derived from ConfigsDirectory, so it belongs here rather than in
// pkg/config: that package holds the schema and must stay importable
// without this one.
func GeneralConfigFilePath() string {
	return path.Join(ConfigsDirectory, "general.yaml")
}

// SecretsConfigFilePath is where secrets.yaml lives for this run.
func SecretsConfigFilePath() string {
	return path.Join(ConfigsDirectory, "secrets.yaml")
}
