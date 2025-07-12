package constants

var (
	CommonRuntimeDependencies = []string{
		"kubeseal",
		"kubectl",

		// Required to build KubePrometheus.
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
