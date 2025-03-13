package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type Azure struct {
	credentials *azidentity.DefaultAzureCredential
}

func NewAzureCloudProvider(ctx context.Context) cloud.CloudProvider {
	credentials, err := azidentity.NewDefaultAzureCredential(nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure credentials")

	return &Azure{credentials}
}
