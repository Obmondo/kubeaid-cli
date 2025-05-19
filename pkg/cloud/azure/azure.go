package azure

import (
	"context"
	"log/slog"

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

	// Create Azure Resource Group, if it doesn't already exist.
	resourceGroupName := config.ParsedGeneralConfig.Cluster.Name
	_, err = resourceGroupsClient.CreateOrUpdate(ctx, resourceGroupName,
		armresources.ResourceGroup{
			Location: &config.ParsedGeneralConfig.Cloud.Azure.Location,
		},
		nil,
	)
	assert.AssertErrNil(ctx, err,
		"Failed creating / updating Resource Group",
		slog.String("name", resourceGroupName),
	)
	slog.InfoContext(ctx, "Created Azure Resource Group", slog.String("name", resourceGroupName))

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
