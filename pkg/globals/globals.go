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

	ArgoCDApplicationClientCloser io.Closer
	ArgoCDApplicationClient       application.ApplicationServiceClient

	// Azure specific.
	CAPIUAMIClientID,
	VeleroUAMIClientID,
	AzureStorageAccountAccessKey string
	IsDebugModeEnabled bool
)
