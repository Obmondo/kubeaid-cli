// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package globals

import (
	"io"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
)

var (
	ConfigsDirectory,

	// KubeaidCLIVersion is the version of the kubeaid-cli binary, injected
	// from the version package via ldflags at build time. Set once at startup
	// in cmd/kubeaid-core/root/root.go. Empty string and "dev" both mean a
	// local / unreleased build; consumers must treat those the same way.
	KubeaidCLIVersion,

	CloudProviderName string
	CloudProvider cloud.CloudProvider

	// When using a VPN cluster, we pre-provision the HCloud control-plane LB.
	// Without a configured hostname, its private IP is rendered as the control-plane endpoint.
	// With a configured hostname, the hostname is rendered as the endpoint and temporarily
	// resolves to this LB's public IP during bootstrap, then to this private IP afterward.
	ControlPlaneLBPrivateIP         string
	ControlPlaneHostname            string
	ControlPlaneLBBootstrapPublicIP string

	// CoturnFloatingIPs holds the HCloud Floating IP(s) kubeaid-cli
	// provisions for NetBird Coturn (STUN/TURN) HA on a multi-CP HCloud
	// VPN cluster. Set during prerequisite-infra (CreateCoturnFloatingIP)
	// and rendered into the capi-cluster chart's controlPlane.hcloud
	// .floatingIPs so each CP binds it via netplan. Empty otherwise.
	CoturnFloatingIPs []string

	ArgoCDApplicationClientCloser io.Closer
	ArgoCDApplicationClient       application.ApplicationServiceClient

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
