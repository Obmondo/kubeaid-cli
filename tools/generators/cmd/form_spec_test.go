// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config/schema"
	"github.com/Obmondo/kubeaid-cli/tools/generators/pkg/sourcefile"
	"github.com/Obmondo/kubeaid-cli/tools/generators/pkg/structs"
)

// loadRealConfigStructs parses the actual pkg/config/general.go and
// pkg/config/secrets.go — the same two files `make run-generators` feeds
// every emitter — so these tests exercise the real tier/validate/when
// tags rather than a synthetic fixture that could drift from them.
func loadRealConfigStructs(t *testing.T) *structs.Structs {
	t.Helper()

	ctx := context.Background()
	gathered := structs.Structs{All: map[string]*structs.Struct{}, Roots: []*structs.Struct{}}

	for _, path := range []string{"../../../pkg/config/general.go", "../../../pkg/config/secrets.go"} {
		sourceFile := sourcefile.NewSourceFile(ctx, path)
		maps.Copy(gathered.All, sourceFile.GetStructs().All)
		gathered.Roots = append(gathered.Roots, sourceFile.GetStructs().Roots...)
	}
	gathered.ResolveEmbeddedStructFields()

	return &gathered
}

// findField returns the field at path, failing the test if not found.
func findField(t *testing.T, spec schema.Spec, path ...string) schema.Field {
	t.Helper()

	for _, section := range spec.Sections {
		for _, field := range section.Fields {
			if slicesEqual(field.Path, path) {
				return field
			}
		}
	}

	t.Fatalf("field %v not found in spec", path)
	return schema.Field{}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBuildFormSpecBasicFieldsAreExactlyTheIntendedSet locks in the
// add-cluster design's "roughly 10, and ONLY those" rule for tier:"basic"
// — cluster name/type/K8s version, the two fork URLs, and Hetzner's
// region/zone + control-plane machine type + replicas + one node group.
// Failing here means a tier:"basic" tag was added or removed without a
// matching, deliberate review of this list.
func TestBuildFormSpecBasicFieldsAreExactlyTheIntendedSet(t *testing.T) {
	spec := buildFormSpec(loadRealConfigStructs(t))

	var basicPaths []string
	for _, section := range spec.Sections {
		for _, field := range section.Fields {
			if field.Tier == structs.TierBasic {
				basicPaths = append(basicPaths, joinPath(field.Path))
			}
		}
	}

	assert.ElementsMatch(t, []string{
		"forkURLs.kubeaid.url",
		"forkURLs.kubeaidConfig.url",
		"cluster.type",
		"cluster.name",
		"cluster.k8sVersion",
		"cloud.hetzner.hcloud.zone",
		"cloud.hetzner.controlPlane.hcloud.machineType",
		"cloud.hetzner.controlPlane.hcloud.replicas",
		"cloud.hetzner.nodeGroups.hcloud",
	}, basicPaths)
}

func joinPath(path []string) string {
	out := path[0]
	for _, segment := range path[1:] {
		out += "." + segment
	}
	return out
}

// TestBuildFormSpecRequiredFieldsSpotCheck exercises every validate-tag
// form actually present in general.go/secrets.go against real fields,
// including the dive boundary: a field with `omitempty,dive,required`
// must itself be optional (only its slice elements are required).
func TestBuildFormSpecRequiredFieldsSpotCheck(t *testing.T) {
	spec := buildFormSpec(loadRealConfigStructs(t))

	tests := []struct {
		path     []string
		required bool
	}{
		{path: []string{"cluster", "name"}, required: true},                                            // notblank
		{path: []string{"cluster", "type"}, required: true},                                            // notblank,oneof=...
		{path: []string{"forkURLs", "kubeaid", "url"}, required: true},                                 // required
		{path: []string{"cloud", "hetzner", "mode"}, required: true},                                   // notblank,oneof=...
		{path: []string{"cloud", "hetzner", "controlPlane", "regions"}, required: true},                // required,min=1,dive,notblank
		{path: []string{"git", "knownHosts"}, required: false},                                         // no validate tag
		{path: []string{"cluster", "acmeEmail"}, required: false},                                      // omitempty,email
		{path: []string{"cluster", "netbird", "groups"}, required: false},                              // omitempty,dive,required — field itself optional
		{path: []string{"cloud", "hetzner", "bareMetal", "firewall", "allowSshFrom"}, required: false}, // omitempty,dive,ipv4|cidrv4
	}

	for _, tc := range tests {
		t.Run(joinPath(tc.path), func(t *testing.T) {
			field := findField(t, spec, tc.path...)
			assert.Equal(t, tc.required, field.Required)
		})
	}
}

// TestBuildFormSpecEnumExtraction proves oneof= options survive into the
// spec in declaration order for both a two-way and a three-way enum.
func TestBuildFormSpecEnumExtraction(t *testing.T) {
	spec := buildFormSpec(loadRealConfigStructs(t))

	assert.Equal(t, []string{"vpn", "workload"}, findField(t, spec, "cluster", "type").Enum)
	assert.Equal(t, []string{"bare-metal", "hcloud", "hybrid"}, findField(t, spec, "cloud", "hetzner", "mode").Enum)
	assert.Nil(t, findField(t, spec, "cluster", "name").Enum)
}

// TestBuildFormSpecAppliesWhenInheritance proves a `when` tag declared on
// an ancestor field (e.g. ClusterConfig.Keycloak, HetznerConfig.HCloud)
// propagates down to every descendant leaf, a leaf can carry its own
// `when` tag directly (the two node-group slices, gated on Hetzner mode
// independently of their ancestor), a `when` tag can reference a field on
// the other config root (secrets.yaml's NetBirdBackendClientSecret gated
// on general.yaml's cluster.keycloak.mode), and a field with no `when`
// tag anywhere in its ancestry carries a nil AppliesWhen.
func TestBuildFormSpecAppliesWhenInheritance(t *testing.T) {
	spec := buildFormSpec(loadRealConfigStructs(t))

	// Inherited from HetznerConfig.HCloud's own `when` tag.
	assert.Equal(t,
		map[string][]string{"cloud.hetzner.mode": {"hcloud", "hybrid"}},
		findField(t, spec, "cloud", "hetzner", "hcloud", "zone").AppliesWhen,
	)

	// Inherited from ClusterConfig.Keycloak's own `when` tag.
	assert.Equal(t,
		map[string][]string{"cluster.type": {"vpn"}},
		findField(t, spec, "cluster", "keycloak", "dns").AppliesWhen,
	)

	// Declared directly on the leaf (HetznerNodeGroups.BareMetal), not
	// inherited — its parent (HetznerConfig.NodeGroups) has no `when`.
	assert.Equal(t,
		map[string][]string{"cloud.hetzner.mode": {"bare-metal", "hybrid"}},
		findField(t, spec, "cloud", "hetzner", "nodeGroups", "bareMetal").AppliesWhen,
	)

	// secrets.yaml field gated on a general.yaml path.
	assert.Equal(t,
		map[string][]string{"cluster.keycloak.mode": {"external"}},
		findField(t, spec, "keycloak", "netBirdBackendClientSecret").AppliesWhen,
	)

	assert.Nil(t, findField(t, spec, "cluster", "name").AppliesWhen)
}

// TestBuildFormSpecAppliesWhenPathsResolve proves every appliesWhen key
// in the spec is a real field's Path, joined with ".". whenCondition
// builds AppliesWhen straight from the raw `when` struct-tag string with
// no cross-validation against the rest of the model — this is the test
// that would catch a `when` tag left stale after the field it points at
// is renamed or removed, the same class of drift caught elsewhere in
// this repo by grepping template field refs before a struct rename.
func TestBuildFormSpecAppliesWhenPathsResolve(t *testing.T) {
	spec := buildFormSpec(loadRealConfigStructs(t))

	knownPaths := map[string]bool{}
	for _, section := range spec.Sections {
		for _, field := range section.Fields {
			knownPaths[joinPath(field.Path)] = true
		}
	}

	checked := map[string]bool{}
	for _, section := range spec.Sections {
		for _, field := range section.Fields {
			for whenPath := range field.AppliesWhen {
				if checked[whenPath] {
					continue
				}
				checked[whenPath] = true
				assert.True(t, knownPaths[whenPath],
					"appliesWhen references %q, which is not any field's path in the spec", whenPath)
			}
		}
	}
	assert.NotEmpty(t, checked, "expected at least one appliesWhen condition to check")
}

// TestBuildFormSpecJSONRoundTrips proves the spec survives an
// encode/decode cycle unchanged — the same path pkg/config/schema.FormSpec
// takes at runtime over the embedded formspec.json.
func TestBuildFormSpecJSONRoundTrips(t *testing.T) {
	spec := buildFormSpec(loadRealConfigStructs(t))

	encoded, err := json.Marshal(spec)
	require.NoError(t, err)

	var decoded schema.Spec
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, spec, decoded)
}
