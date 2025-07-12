package utils

import (
	"context"
	"log/slog"
	"os/exec"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Determines the runtime dependencies required by KubeAid Bootstrap Script, based on the
// cloud-provider being used.
// Panics if any of them are not found in PATH.
func EnsureRuntimeDependenciesInstalled(ctx context.Context) {
	// Determine required runtime dependencies, based on the cloud-provider being used.
	dependencies := constants.CommonRuntimeDependencies
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		break

	case constants.CloudProviderAzure:
		dependencies = append(dependencies, constants.AzureSpecificRuntimeDependencies...)

	case constants.CloudProviderHetzner:
		break

	case constants.CloudProviderBareMetal:
		dependencies = append(dependencies, constants.BareMetalSpecificRuntimeDependencies...)
	}

	// Ensure that each of those runtime dependencies are installed.
	for _, dependency := range dependencies {
		ensureRuntimeDependencyInstalled(ctx, dependency)
	}
}

// Panics if the given runtime dependency / executable isn't found in PATH.
func ensureRuntimeDependencyInstalled(ctx context.Context, name string) {
	_, err := exec.LookPath(name)
	assert.AssertErrNil(ctx, err,
		"Runtime dependency unavailable",
		slog.String("runtime-dependency", name),
	)
}
