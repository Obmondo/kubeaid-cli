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

	ArgoCDApplicationClientCloser io.Closer
	ArgoCDApplicationClient       application.ApplicationServiceClient

	// Azure specific.
	CAPIUAMIClientID,
	VeleroUAMIClientID,
	AzureStorageAccountAccessKey string
	IsDebugModeEnabled bool
)
