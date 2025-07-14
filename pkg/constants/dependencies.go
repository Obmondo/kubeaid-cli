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

	BareMetalSpecificRuntimeDependencies = []string{
		"kubeone",
	}
)
