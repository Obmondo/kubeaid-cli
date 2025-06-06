package constants

var (
	CommonRuntimeDependencies = []string{
		"k3d",
		"kubeseal",
		"kubectl",

		"jsonnet",
		"jb",
		"jq",
		"gojsontoyaml",

		"yq",
	}

	AWSSpecificRuntimeDependencies = []string{
		"clusterctl",
		"clusterawsadm",
	}

	AzureSpecificRuntimeDependencies = []string{
		"clusterctl",
		"az",
		"azwi",
	}

	HetznerSpecificRuntimeDependencies = []string{
		"clusterctl",
	}
)
