package globals

import (
	"io"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	azureTypes "github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure/types"
)

var (
	ConfigsDirectory,

	TempDir,

	CloudProviderName string
	CloudProvider cloud.CloudProvider

	ArgoCDApplicationClientCloser io.Closer
	ArgoCDApplicationClient       application.ApplicationServiceClient

	// Azure specific.

	WorkloadIdentityInfrastructureStatus *azureTypes.WorkloadIdentityInfrastructureStatus
	DisasterRecoveryInfrastructureStatus *azureTypes.DisasterRecoveryInfrastructureStatus
)
