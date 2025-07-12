package constants

var (
	CommonRuntimeDependencies = []string{
		// Required to build KubePrometheus.
		"jsonnet",
		"jb",
		"jq",
		"gojsontoyaml",

		"kubectl",
	}

	AzureSpecificRuntimeDependencies = []string{
		"az",
	}

	BareMetalSpecificRuntimeDependencies = []string{
		"kubeone",
	}
)
