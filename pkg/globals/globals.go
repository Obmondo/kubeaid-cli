package globals

import (
	"io"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
)

var (
	TempDir string

	CloudProviderName string
	CloudProvider     cloud.CloudProvider

	ArgoCDApplicationClientCloser io.Closer
	ArgoCDApplicationClient       application.ApplicationServiceClient

	// Azure specific.
	UserAssignedIdentityClientID string
)
