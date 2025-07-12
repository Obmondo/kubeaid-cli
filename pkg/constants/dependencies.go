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

	AzureSpecificRuntimeDependencies = []string{
		"az",
	}
)
