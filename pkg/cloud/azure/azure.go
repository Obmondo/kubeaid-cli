package azure

import (
	"context"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Azure struct {
	credentials *azidentity.ClientSecretCredential
	subscriptionID,
	resourceGroupName string
	resourceGroupsClient *armresources.ResourceGroupsClient
	vmSizesClient        *armcompute.VirtualMachineSizesClient
}

func NewAzureCloudProvider() cloud.CloudProvider {
	ctx := context.Background()

	azureConfig := config.ParsedConfig.Cloud.Azure

	credentials, err := azidentity.NewClientSecretCredential(
		azureConfig.TenantID,
		azureConfig.ClientID,
		azureConfig.ClientSecret,
		nil,
	)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure credentials")

	subscriptionID := azureConfig.SubscriptionID

	armClientFactory, err := armresources.NewClientFactory(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure Resource Manager (ARM) client factory")

	resourceGroupsClient := armClientFactory.NewResourceGroupsClient()

	vmSizesClient, err := armcompute.NewVirtualMachineSizesClient(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure VM sizes client")

	// Create Azure Resource Group, if it doesn't already exist.
	resourceGroupName := config.ParsedConfig.Cluster.Name
	{
		_, err = resourceGroupsClient.CreateOrUpdate(ctx, resourceGroupName,
			armresources.ResourceGroup{
				Location: &config.ParsedConfig.Cloud.Azure.Location,
			},
			nil,
		)
		assert.AssertErrNil(ctx, err,
			"Failed creating / updating Resource Group",
			slog.String("name", resourceGroupName),
		)

		slog.InfoContext(ctx, "Created Azure Resource Group", slog.String("resource-group-name", resourceGroupName))
	}

	return &Azure{
		credentials,
		subscriptionID,
		resourceGroupName,
		resourceGroupsClient,
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
