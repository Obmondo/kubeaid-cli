package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/google/uuid"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type CreateUAMIArgs struct {
	UAMIClient            *armmsi.UserAssignedIdentitiesClient
	RoleAssignmentsClient *armauthorization.RoleAssignmentsClient

	SubscriptionID,
	ResourceGroupName,

	RoleID,
	RoleAssignmentScope,
	Name string
}

// Creates a User Assigned Managed Identity (UAMI), if it doesn't already exist.
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
func CreateUAMI(ctx context.Context,
	args CreateUAMIArgs,
) (uamiID string, uamiClientID string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("uami-name", args.Name),
	})

	azureConfig := config.ParsedGeneralConfig.Cloud.Azure

	slog.InfoContext(ctx, "Creating UAMI")
	response, err := args.UAMIClient.CreateOrUpdate(ctx,
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
	assert.AssertErrNil(ctx, err, "Failed creating UAMI")

	uamiID = *response.Properties.PrincipalID
	uamiClientID = *response.Properties.ClientID

	// Assign role to the UAMI.
	{
		ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("role-id", args.RoleID),
		})

		var (
			roleDefinitionID = fmt.Sprintf(
				"/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s",
				azureConfig.SubscriptionID,
				constants.AzureRoleIDContributor,
			)
			roleAssignmentID = uuid.New().String()
		)

		slog.InfoContext(ctx, "Assigning role to UAMI")
		err := utils.WithRetry(10*time.Second, 6, func() error {
			_, err := args.RoleAssignmentsClient.Create(ctx,
				args.RoleAssignmentScope,
				roleAssignmentID,
				armauthorization.RoleAssignmentCreateParameters{
					Properties: &armauthorization.RoleAssignmentProperties{
						PrincipalID:      &uamiID,
						RoleDefinitionID: &roleDefinitionID,
					},
				},
				nil,
			)
			if err != nil {
				// Skip, if the role is already assigned to the User Assigned Managed Identity.
				//nolint:errorlint
				responseError, ok := err.(*azcore.ResponseError)
				if ok &&
					responseError.StatusCode == constants.AzureResponseStatusCodeResourceAlreadyExists {
					slog.InfoContext(ctx, "Role is already assigned to UAMI")
					return nil
				}
			}
			return err
		})
		assert.AssertErrNil(ctx, err, "Failed assigning role to UAMI")
	}

	return
}
