package config

import (
	_ "unsafe"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
)

// We need to do this to avoid `import cycle not allowed` error.

//go:linkname NewAWSCloudProvider github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws.NewAWSCloudProvider
func NewAWSCloudProvider() cloud.CloudProvider

//go:linkname NewAzureCloudProvider github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure.NewAzureCloudProvider
func NewAzureCloudProvider() cloud.CloudProvider
