// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/profile/p20200901/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
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

	// listVMSizes fetches all VM sizes for a given location.
	// Injected at construction time; tests may replace it.
	listVMSizes func(ctx context.Context, location string) ([]*armcompute.VirtualMachineSize, error)

	// deleteResourceGroupFn deletes a resource group and waits for completion.
	// Injected at construction time; tests may replace it.
	deleteResourceGroupFn func(ctx context.Context, name string) error

	// pollInterval controls the polling frequency for infrastructure readiness.
	// Defaults to time.Minute; tests may override with a shorter duration.
	pollInterval time.Duration
}

func NewAzureCloudProvider() (cloud.CloudProvider, error) {
	credentials, err := GetClientSecretCredentials()
	if err != nil {
		return nil, fmt.Errorf("getting client secret credentials: %w", err)
	}

	subscriptionID := config.ParsedGeneralConfig.Cloud.Azure.SubscriptionID

	armClientFactory, err := armresources.NewClientFactory(subscriptionID, credentials, nil)
	if err != nil {
		return nil, fmt.Errorf("constructing Azure Resource Manager (ARM) client factory: %w", err)
	}

	resourceGroupsClient := armClientFactory.NewResourceGroupsClient()

	vmSizesClient, err := armcompute.NewVirtualMachineSizesClient(subscriptionID, credentials, nil)
	if err != nil {
		return nil, fmt.Errorf("constructing Azure VM sizes client: %w", err)
	}

	uamiClient, err := armmsi.NewUserAssignedIdentitiesClient(
		subscriptionID, credentials, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating User Assigned Identities client: %w", err)
	}

	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(
		subscriptionID, credentials, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating Role Assignments client: %w", err)
	}

	storageClientFactory, err := armstorage.NewClientFactory(subscriptionID, credentials, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Storage client factory: %w", err)
	}

	resourceGroupName := config.ParsedGeneralConfig.Cluster.Name

	azure := &Azure{
		credentials: credentials,

		subscriptionID:    subscriptionID,
		resourceGroupName: resourceGroupName,

		resourceGroupsClient:  resourceGroupsClient,
		vmSizesClient:         vmSizesClient,
		uamiClient:            uamiClient,
		roleAssignmentsClient: roleAssignmentsClient,
		storageClientFactory:  storageClientFactory,
	}

	azure.pollInterval = time.Minute

	azure.listVMSizes = func(ctx context.Context, location string) ([]*armcompute.VirtualMachineSize, error) {
		pager := vmSizesClient.NewListPager(location, nil)
		var sizes []*armcompute.VirtualMachineSize
		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			sizes = append(sizes, resp.Value...)
		}
		return sizes, nil
	}

	azure.deleteResourceGroupFn = func(ctx context.Context, name string) error {
		poller, err := resourceGroupsClient.BeginDelete(ctx, name, nil)
		if err != nil {
			return fmt.Errorf("initiating deletion of Azure Resource Group: %w", err)
		}
		_, err = poller.PollUntilDone(ctx, nil)
		if err != nil {
			return fmt.Errorf("waiting for deletion of Azure Resource Group: %w", err)
		}
		return nil
	}

	return azure, nil
}
