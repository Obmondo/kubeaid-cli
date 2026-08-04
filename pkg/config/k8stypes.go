// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

// The two types below mirror k8s.io/api/core/v1 so this package stays
// importable from a service that has no business talking to a
// kube-apiserver — obmondo-api-go renders and validates this config, and
// imports no k8s libraries at all. Importing coreV1 for two field types
// would cost it 78 packages.
//
// Mirroring an upstream type normally risks drift. These two are safe:
// HostPathType is a bare string, and Taint is one of the oldest and most
// stable shapes in core/v1. TestMirroredK8sTypesMatchUpstream marshals both
// against the real thing, so a divergence fails a test rather than
// reaching a cluster.

// HostPathType mirrors coreV1.HostPathType.
type HostPathType string

// TaintEffect mirrors coreV1.TaintEffect.
type TaintEffect string

// Taint mirrors coreV1.Taint, minus TimeAdded.
//
// TimeAdded is set by the kubelet when it applies a taint; it is meaningless
// in a config file a human writes, and coreV1 marks it omitempty, so
// omitting it here keeps the marshalled output identical.
type Taint struct {
	Key    string      `json:"key"              yaml:"key"`
	Value  string      `json:"value,omitempty"  yaml:"value,omitempty"`
	Effect TaintEffect `json:"effect"           yaml:"effect"`
}
