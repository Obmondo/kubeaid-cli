package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type Azure struct {
	credentials *azidentity.ClientSecretCredential

	subscriptionID,
	resourceGroupName string

	resourceGroupsClient  *armresources.ResourceGroupsClient
	vmSizesClient         *armcompute.VirtualMachineSizesClient
	uamiClient            *armmsi.UserAssignedIdentitiesClient
	roleAssignmentsClient *armauthorization.RoleAssignmentsClient
	storageClientFactory  *armstorage.ClientFactory
}

func NewAzureCloudProvider() cloud.CloudProvider {
	ctx := context.Background()

	credentials := GetClientSecretCredentials(ctx)

	subscriptionID := config.ParsedGeneralConfig.Cloud.Azure.SubscriptionID

	armClientFactory, err := armresources.NewClientFactory(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure Resource Manager (ARM) client factory")

	resourceGroupsClient := armClientFactory.NewResourceGroupsClient()

	vmSizesClient, err := armcompute.NewVirtualMachineSizesClient(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed constructing Azure VM sizes client")

	uamiClient, err := armmsi.NewUserAssignedIdentitiesClient(
		subscriptionID, credentials, nil,
	)
	assert.AssertErrNil(ctx, err, "Failed creating User Assigned Identities client")

	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(
		subscriptionID, credentials, nil,
	)
	assert.AssertErrNil(ctx, err, "Failed creating Role Assignments client")

	storageClientFactory, err := armstorage.NewClientFactory(subscriptionID, credentials, nil)
	assert.AssertErrNil(ctx, err, "Failed creating Azure Storage client factory")

	resourceGroupName := config.ParsedGeneralConfig.Cluster.Name

	return &Azure{
		credentials,

		subscriptionID,
		resourceGroupName,

		resourceGroupsClient,
		vmSizesClient,
		uamiClient,
		roleAssignmentsClient,
		storageClientFactory,
	}
}
