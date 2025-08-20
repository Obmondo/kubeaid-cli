// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

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
