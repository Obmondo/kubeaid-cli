package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type Azure struct {
	credentials      *azidentity.DefaultAzureCredential
	subscriptionID   string
	armClientFactory *armresources.ClientFactory
}

func NewAzureCloudProvider(ctx context.Context) cloud.CloudProvider {
	credentials, err := azidentity.NewDefaultAzureCredential(nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure credentials")

	subscriptionID := config.ParsedConfig.Cloud.Azure.SubscriptionID

	armClientFactory, err := armresources.NewClientFactory(subscriptionID, a.credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure Resource Manager (ARM) client factory")

	return &Azure{
		credentials,
		subscriptionID,
		armClientFactory,
	}
}
