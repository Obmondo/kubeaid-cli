// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

// Change reports what an Ensure* method did (or, in dry-run mode,
// would do) to one Keycloak object.
type Change int

const (
	// ChangeNone means the object already matched the desired state.
	ChangeNone Change = iota
	// ChangeCreated means the object was (or would be) created.
	ChangeCreated
	// ChangeUpdated means an existing object was (or would be) changed.
	ChangeUpdated
)

func (c Change) String() string {
	switch c {
	case ChangeNone:
		return "ok"
	case ChangeCreated:
		return "create"
	case ChangeUpdated:
		return "update"
	}
	return "unknown"
}

// Result is one reconciled object: its kind ("client", "group",
// "client-mapper", ...), a human-readable name, the change and an
// optional detail naming the drifted fields. Details never carry
// secret values.
type Result struct {
	Kind   string
	Name   string
	Change Change
	Detail string
}
