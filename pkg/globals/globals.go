// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package globals

import (
	"io"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
)

var (
	ConfigsDirectory,

	CloudProviderName string
	CloudProvider cloud.CloudProvider

	// When using a VPN cluster, we pre-provision an internal LB : LB with just private IP.
	// That LB IP is then specified as the control-plane endpoint to CAPI and Cilium.
	// This way, the control-plane endpoint isn't exposed to the public internet.
	PreProvisionedControlPlaneLBIP string

	ArgoCDApplicationClientCloser io.Closer
	ArgoCDApplicationClient       application.ApplicationServiceClient

	// Azure specific.
	CAPIUAMIClientID,
	VeleroUAMIClientID,
	AzureStorageAccountAccessKey string
	IsDebugModeEnabled bool
)
