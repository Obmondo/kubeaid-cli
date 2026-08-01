// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package structs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// threeLevelEmbedChain mirrors the real shape that made
// ResolveEmbeddedStructFields' resolution order depend on Go's
// (randomized) map iteration order: HCloudAutoScalableNodeGroup embeds
// AutoScalableNodeGroup, which itself embeds NodeGroup — a struct that
// is both an embedder and embedded elsewhere.
func threeLevelEmbedChain() *Structs {
	return &Structs{
		All: map[string]*Struct{
			"NodeGroup": {
				Name:   "NodeGroup",
				Fields: []Field{{Name: "name", Type: "string"}},
			},
			"AutoScalableNodeGroup": {
				Name: "AutoScalableNodeGroup",
				Fields: []Field{
					{Name: "NodeGroup", Type: "NodeGroup", Embedded: true},
					{Name: "minSize", Type: "uint"},
				},
			},
			"HCloudAutoScalableNodeGroup": {
				Name: "HCloudAutoScalableNodeGroup",
				Fields: []Field{
					{Name: "AutoScalableNodeGroup", Type: "AutoScalableNodeGroup", Embedded: true},
					{Name: "machineType", Type: "string"},
				},
			},
		},
	}
}

func fieldNames(fields []Field) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return names
}

// TestResolveEmbeddedStructFieldsIsDeterministic locks in the exact
// resolved field order for a struct that both embeds and is itself
// embedded — own fields first (embed marker removed, order otherwise
// unchanged), promoted fields appended after. Regression test for the
// map-iteration-order bug fixed by iterating structs.Sorted(): run
// against a Go map, this order was not guaranteed to be the same from
// one run to the next.
func TestResolveEmbeddedStructFieldsIsDeterministic(t *testing.T) {
	all := threeLevelEmbedChain()
	all.ResolveEmbeddedStructFields()

	assert.Equal(t,
		[]string{"minSize", "name"},
		fieldNames(all.All["AutoScalableNodeGroup"].Fields),
	)
	assert.Equal(t,
		[]string{"machineType", "minSize", "name"},
		fieldNames(all.All["HCloudAutoScalableNodeGroup"].Fields),
	)
	assert.Equal(t,
		[]string{"name"},
		fieldNames(all.All["NodeGroup"].Fields),
	)
}

// TestResolveEmbeddedStructFieldsIsStableAcrossRuns rebuilds the same
// input from scratch and resolves it repeatedly — each run constructs a
// fresh map (a new random iteration order), so a result that varies
// between runs would mean the determinism fix regressed.
func TestResolveEmbeddedStructFieldsIsStableAcrossRuns(t *testing.T) {
	var want []string
	for i := 0; i < 20; i++ {
		all := threeLevelEmbedChain()
		all.ResolveEmbeddedStructFields()
		got := fieldNames(all.All["HCloudAutoScalableNodeGroup"].Fields)

		if want == nil {
			want = got
			continue
		}
		assert.Equal(t, want, got, "run %d produced a different field order", i)
	}
}
