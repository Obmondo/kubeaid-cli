// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

func TestFormSpec(t *testing.T) {
	spec, err := FormSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	assert.NotEmpty(t, spec.Sections)

	// Version is stamped from globals.KubeaidCLIVersion (root.go sets it
	// from ldflags at CLI startup; unset — "" — in a package test).
	assert.Equal(t, globals.KubeaidCLIVersion, spec.Version)

	// A known basic field, present with the expected shape — proves the
	// embedded formspec.json actually decodes into Spec correctly, not
	// just that json.Unmarshal returned no error.
	var clusterName *Field
	for _, section := range spec.Sections {
		for _, field := range section.Fields {
			if len(field.Path) == 2 && field.Path[0] == "cluster" && field.Path[1] == "name" {
				f := field
				clusterName = &f
			}
		}
	}
	require.NotNil(t, clusterName, "cluster.name should be present in the spec")
	assert.Equal(t, "basic", clusterName.Tier)
	assert.True(t, clusterName.Required)
	assert.Equal(t, "string", clusterName.Type)
}
