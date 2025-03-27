package azure

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Type casts the give CloudProvider interface instance to an instance of the Azure struct.
// Panics if the type casting fails.
func CloudProviderToAzure(ctx context.Context, cloudProvider cloud.CloudProvider) *Azure {
	azure, ok := cloudProvider.(*Azure)
	assert.Assert(ctx, ok, "Failed type casting CloudProvider interface to Azure struct type")

	return azure
}
