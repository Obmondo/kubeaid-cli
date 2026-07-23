// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// capiValuesFacts is the subset of a rendered values-capi-cluster.yaml that decides
// whether a change can be reconciled onto a running cluster, and how it will land.
//
// Only HCloud is modelled. Bare-metal workers are Machine objects rather than
// MachineDeployments, so they never rotate a template.
type capiValuesFacts struct {
	// MachineTemplateRotation names MachineTemplates after a hash of their spec.
	MachineTemplateRotation bool

	// ImageName and the machine types below feed that hash. Changing any of them
	// rotates a template name, which is the only thing ClusterAPI reacts to.
	ImageName            string
	ControlPlaneType     string
	ControlPlaneReplicas int
	NodeGroupTypes       map[string]string
}

// capiValuesSyncPlan describes what a values-capi-cluster.yaml change will do to a
// running cluster.
type capiValuesSyncPlan struct {
	// RollingChanges rotate a MachineTemplate name, so ClusterAPI replaces machines.
	RollingChanges []string

	// ReplicaChange scales the control plane. New members join etcd; nothing is
	// replaced. Empty when the replica count is unchanged.
	ReplicaChange string

	// RotationNewlyEnabled means the templates are about to be renamed for the first
	// time. The rename alone rolls the control plane, even with an unchanged spec.
	RotationNewlyEnabled bool

	// RotationEnabled is the state after the change.
	RotationEnabled bool

	// ControlPlaneReplicasAfter is the replica count the control plane will roll with.
	ControlPlaneReplicasAfter int
}

// rolls reports whether ClusterAPI will actually replace machines.
//
// A rewritten template spec only rolls when rotation is on to rename the template — with
// the fixed legacy name ClusterAPI sees no change at all. That case is a refusal, not a
// roll, and must not be described to the operator as one.
func (p *capiValuesSyncPlan) rolls() bool {
	if !p.RotationEnabled {
		return false
	}

	return len(p.RollingChanges) > 0 || p.RotationNewlyEnabled
}

// Refusals lists the reasons this change must not be applied as a single sync.
//
// Each is a change that would either do nothing at all, or roll the control plane in
// a way that risks etcd quorum. They are hard stops rather than warnings because both
// failure modes are silent: ClusterAPI reports success in the first case, and a lost
// quorum is not recoverable by re-running the command.
func (p *capiValuesSyncPlan) Refusals() []string {
	var refusals []string

	// A rotating change with rotation off never reaches the cluster. ClusterAPI decides
	// a Machine is up to date by comparing the name of the template it was cloned from
	// against the name its owner references — never the template's contents. With the
	// fixed legacy name, the changed spec applies only to machines created later.
	if len(p.RollingChanges) > 0 && !p.RotationEnabled {
		refusals = append(refusals, fmt.Sprintf(
			"This change rewrites a MachineTemplate spec :\n\n%s\n\n"+
				"but cluster.machineTemplateRotation is false, so the template keeps its fixed\n"+
				"name. ClusterAPI compares only a template's name when deciding whether a Machine\n"+
				"is up to date, so nothing would roll : the new spec would apply silently to\n"+
				"machines created later, leaving the cluster with mixed instance types.\n\n"+
				"Set cluster.machineTemplateRotation: true in general.yaml first. Note that\n"+
				"enabling it renames the template on its own, which rolls the control plane once.",
			indentLines(p.RollingChanges, "  - "),
		))
	}

	// Scaling and rolling at once means ClusterAPI both adds members and replaces them.
	// Starting from a single control-plane node that walks etcd through a sequence of
	// membership changes with no quorum to lose.
	if p.rolls() && len(p.ReplicaChange) > 0 {
		refusals = append(refusals, fmt.Sprintf(
			"This change both scales the control plane (%s) and rolls it :\n\n%s\n\n"+
				"Split it into two syncs. Scale out first — the MachineTemplate spec is untouched,\n"+
				"so the new members simply join etcd and nothing is replaced. Once they are Ready,\n"+
				"apply the rolling change against a control plane that has quorum to spare.",
			p.ReplicaChange, indentLines(p.rollReasons(), "  - "),
		))
	}

	return refusals
}

// Warnings lists things worth an explicit second look, but which are safe to proceed with.
func (p *capiValuesSyncPlan) Warnings() []string {
	if !p.rolls() {
		return nil
	}

	// A rolling replacement surges one machine, waits for it to join, then removes an old
	// one. Below three replicas every intermediate etcd membership is one node away from
	// losing quorum.
	if p.ControlPlaneReplicasAfter < 3 {
		return []string{fmt.Sprintf(
			"The control plane will be rolled with %d replica(s). ClusterAPI adds one machine,\n"+
				"waits for it to join etcd, then removes an old one — with fewer than 3 replicas\n"+
				"every step of that sequence is a single node away from losing quorum.\n"+
				"Scaling to 3 first is strongly recommended.",
			p.ControlPlaneReplicasAfter,
		)}
	}

	return nil
}

// rollReasons describes every reason machines will be replaced.
func (p *capiValuesSyncPlan) rollReasons() []string {
	reasons := make([]string, len(p.RollingChanges), len(p.RollingChanges)+1)
	copy(reasons, p.RollingChanges)

	if p.RotationNewlyEnabled {
		reasons = append(reasons, "machineTemplateRotation enabled (renames the template, which rolls it once)")
	}

	return reasons
}

// planCapiValuesSync compares two rendered values-capi-cluster.yaml files and works out
// how ClusterAPI will react to the change.
func planCapiValuesSync(before, after []byte) (*capiValuesSyncPlan, error) {
	oldFacts, err := parseCapiValuesFacts(before)
	if err != nil {
		return nil, fmt.Errorf("parsing the current values-capi-cluster.yaml: %w", err)
	}

	newFacts, err := parseCapiValuesFacts(after)
	if err != nil {
		return nil, fmt.Errorf("parsing the re-rendered values-capi-cluster.yaml: %w", err)
	}

	plan := &capiValuesSyncPlan{
		RotationEnabled:           newFacts.MachineTemplateRotation,
		RotationNewlyEnabled:      newFacts.MachineTemplateRotation && !oldFacts.MachineTemplateRotation,
		ControlPlaneReplicasAfter: newFacts.ControlPlaneReplicas,
	}

	if oldFacts.ImageName != newFacts.ImageName {
		plan.RollingChanges = append(plan.RollingChanges, fmt.Sprintf(
			"hcloud.imageName : %s -> %s", oldFacts.ImageName, newFacts.ImageName,
		))
	}

	if oldFacts.ControlPlaneType != newFacts.ControlPlaneType {
		plan.RollingChanges = append(plan.RollingChanges, fmt.Sprintf(
			"controlPlane.hcloud.machineType : %s -> %s",
			oldFacts.ControlPlaneType, newFacts.ControlPlaneType,
		))
	}

	for _, name := range sortedKeys(newFacts.NodeGroupTypes) {
		oldType, existed := oldFacts.NodeGroupTypes[name]
		if !existed || oldType == newFacts.NodeGroupTypes[name] {
			continue
		}

		plan.RollingChanges = append(plan.RollingChanges, fmt.Sprintf(
			"nodeGroups.hcloud[%s].machineType : %s -> %s",
			name, oldType, newFacts.NodeGroupTypes[name],
		))
	}

	if oldFacts.ControlPlaneReplicas != newFacts.ControlPlaneReplicas {
		plan.ReplicaChange = fmt.Sprintf("%d -> %d",
			oldFacts.ControlPlaneReplicas, newFacts.ControlPlaneReplicas,
		)
	}

	return plan, nil
}

// parseCapiValuesFacts pulls the rotation-relevant fields out of a rendered
// values-capi-cluster.yaml. Missing fields are left at their zero value: a
// non-Hetzner or bare-metal cluster simply yields no rolling changes.
func parseCapiValuesFacts(values []byte) (*capiValuesFacts, error) {
	var root struct {
		Global struct {
			MachineTemplateRotation bool `json:"machineTemplateRotation"`
		} `json:"global"`

		Hetzner struct {
			HCloud struct {
				ImageName string `json:"imageName"`
			} `json:"hcloud"`

			ControlPlane struct {
				HCloud struct {
					MachineType string `json:"machineType"`
					Replicas    int    `json:"replicas"`
				} `json:"hcloud"`
			} `json:"controlPlane"`

			NodeGroups struct {
				HCloud []struct {
					Name        string `json:"name"`
					MachineType string `json:"machineType"`
				} `json:"hcloud"`
			} `json:"nodeGroups"`
		} `json:"hetzner"`
	}

	if err := yaml.Unmarshal(values, &root); err != nil {
		return nil, err
	}

	facts := &capiValuesFacts{
		MachineTemplateRotation: root.Global.MachineTemplateRotation,
		ImageName:               root.Hetzner.HCloud.ImageName,
		ControlPlaneType:        root.Hetzner.ControlPlane.HCloud.MachineType,
		ControlPlaneReplicas:    root.Hetzner.ControlPlane.HCloud.Replicas,
		NodeGroupTypes:          map[string]string{},
	}

	for _, nodeGroup := range root.Hetzner.NodeGroups.HCloud {
		facts.NodeGroupTypes[nodeGroup.Name] = nodeGroup.MachineType
	}

	return facts, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func indentLines(lines []string, prefix string) string {
	prefixed := make([]string, 0, len(lines))
	for _, line := range lines {
		prefixed = append(prefixed, prefix+line)
	}

	return strings.Join(prefixed, "\n")
}
