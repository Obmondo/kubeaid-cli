package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Azure struct {
	credentials      *azidentity.DefaultAzureCredential
	subscriptionID   string
	armClientFactory *armresources.ClientFactory
	vmSizesClient    *armcompute.VirtualMachineSizesClient
}

func NewAzureCloudProvider(ctx context.Context) cloud.CloudProvider {
	credentials, err := azidentity.NewDefaultAzureCredential(nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure credentials")

	subscriptionID := config.ParsedConfig.Cloud.Azure.SubscriptionID

	armClientFactory, err := armresources.NewClientFactory(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure Resource Manager (ARM) client factory")

	vmSizesClient, err := armcompute.NewVirtualMachineSizesClient(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure VM sizes client")

	return &Azure{
		credentials,
		subscriptionID,
		armClientFactory,
		vmSizesClient,
	}
}

func (a *Azure) UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
	capiClusterValuesFilePath string,
	_updates any,
) {
}

func (a *Azure) UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, _updates any) {
}

func (a *Azure) GetSealedSecretsBackupBucketName() string {
	panic("unimplemented")
}
