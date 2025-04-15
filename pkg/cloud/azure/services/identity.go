package services

import (
	"context"
	"fmt"
	"log/slog"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/google/uuid"
)

type CreateUserAssignedIdentityArgs struct {
	UserAssignedIdentitiesClient *armmsi.UserAssignedIdentitiesClient
	RoleAssignmentsClient        *armauthorization.RoleAssignmentsClient
	ResourceGroupName,
	Name string
}

// Creates a User Assigned Managed Identity, if it doesn't already exist.
// Returns its ID.
/*
Managed identities for Azure resources eliminate the need to manage credentials in code. You can
use them to get a Microsoft Entra token for your applications. The applications can use the token
when accessing resources that support Microsoft Entra authentication. Azure manages the identity so
you don't have to.

There are two types of managed identities :

	(1) system assigned :

	    System-assigned managed identities have their lifecycle tied to the resource that created
	    them. This identity is restricted to only one resource, and you can grant permissions to
	    the managed identity by using Azure role-based access control (RBAC).

	(2) user assigned :

	    User-assigned managed identities can be used on multiple resources.

	REFERENCE : https://learn.microsoft.com/en-us/entra/identity/managed-identities-azure-resources/how-manage-user-assigned-managed-identities.
*/
func CreateUserAssignedIdentity(ctx context.Context,
	args CreateUserAssignedIdentityArgs,
) (userAssignedIdentityID string, userAssignedIdentityClientID string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("name", args.Name),
	})

	azureConfig := config.ParsedGeneralConfig.Cloud.Azure

	slog.InfoContext(ctx, "Creating User Assigned Identity and assigning Contributor Role to it")

	response, err := args.UserAssignedIdentitiesClient.CreateOrUpdate(ctx,
		args.ResourceGroupName,
		args.Name,
		armmsi.Identity{
			Location: &azureConfig.Location,
			Tags: map[string]*string{
				"cluster": &config.ParsedGeneralConfig.Cluster.Name,
			},
		},
		nil,
	)
	assert.AssertErrNil(ctx, err, "Failed creating User Assigned Identity")

	userAssignedIdentityID = *response.Properties.PrincipalID
	userAssignedIdentityClientID = *response.Properties.ClientID

	// Create a role assignment to give the User Assigned Identity Contributor access to the Azure
	// subscription where the main cluster will be created.
	/*
		The Azure built-in Contributor role grants full access to manage all resources, but does not
		allow you to assign roles in Azure RBAC, manage assignments in Azure Blueprints, or share
		image galleries.

		REFERENCE : https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles.
	*/

	var (
		roleDefinitionID = fmt.Sprintf(
			"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
			azureConfig.SubscriptionID,
			constants.AzureRoleIDContributor,
		)
		roleScope        = path.Join("/subscriptions/", azureConfig.SubscriptionID)
		roleAssignmentID = uuid.New().String()
	)

	_, err = args.RoleAssignmentsClient.Create(ctx,
		roleScope,
		roleAssignmentID,
		armauthorization.RoleAssignmentCreateParameters{
			Properties: &armauthorization.RoleAssignmentProperties{
				PrincipalID:      &userAssignedIdentityID,
				RoleDefinitionID: &roleDefinitionID,
			},
		},
		nil,
	)
	if err != nil {
		// Skip, if the Contributor role is already assigned to the User Assigned Managed Identity.
		responseError, ok := err.(*azcore.ResponseError)
		if ok && responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
			slog.InfoContext(ctx, "Contributor role is already assigned to Azure User Assigned Managed Identity")
			return
		}

		assert.AssertErrNil(ctx, err, "Failed creating Role assignment for User Assigned Identity")
	}

	return
}
