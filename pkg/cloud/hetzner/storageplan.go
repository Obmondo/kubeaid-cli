// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/storageplanner"
	"github.com/Obmondo/kubeaid-cli/pkg/storageplanner/storageplan"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/commandexecutor"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

func (h *Hetzner) GenerateStoragePlans(ctx context.Context, hetznerConfig *config.HetznerConfig) error {
	allStoragePlans := make(storageplan.StoragePlans)

	privateKey := hetznerConfig.SSHKeyPair.PrivateKey

	if config.ControlPlaneInHetznerBareMetal() {
		nodeGroupCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", "control-plane"),
		})

		storagePlans := make([]*storageplan.StoragePlan,
			len(hetznerConfig.ControlPlane.BareMetal.BareMetalHosts),
		)

		for i, host := range hetznerConfig.ControlPlane.BareMetal.BareMetalHosts {
			nodeCtx := logger.AppendSlogAttributesToCtx(nodeGroupCtx, []slog.Attr{
				slog.String("server-id", host.ServerID),
			})

			// Pass VG0.Size (the whole VG footprint), not RootVolumeSize:
			// the on-node planner (`kubeaid-storagectl plan execute
			// --os-size {{ vg0.size }}` in the chart's preKubeadm script)
			// reserves the full VG, and unused VG space is LVM headroom —
			// not capacity Ceph/ZFS can claim — so reserving less here
			// over-reports Ceph capacity in the operator prompt.
			sp, err := h.generateStoragePlan(nodeCtx,
				host,
				privateKey,
				hetznerConfig.BareMetal.InstallImage.VG0.Size,
				hetznerConfig.BareMetal.ZFS.Size,
			)
			if err != nil {
				return fmt.Errorf("control-plane server %s: %w", host.ServerID, err)
			}
			storagePlans[i] = sp

			host.WWNs = collectAndSortWWNs(sp.OS)
		}

		if err := storageplan.CheckStoragePlansAlike(storagePlans); err != nil {
			return fmt.Errorf("control-plane storage plans aren't alike: %w", err)
		}

		allStoragePlans["control-plane"] = storagePlans
		hetznerConfig.ControlPlane.BareMetal.StoragePlan = *storagePlans[0]
	}

	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		nodeGroupCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", nodeGroup.Name),
		})

		storagePlans := make([]*storageplan.StoragePlan, len(nodeGroup.BareMetalHosts))

		for i, host := range nodeGroup.BareMetalHosts {
			nodeCtx := logger.AppendSlogAttributesToCtx(nodeGroupCtx, []slog.Attr{
				slog.String("server-id", host.ServerID),
			})

			// Same VG0.Size vs RootVolumeSize rationale as the
			// control-plane branch above — see that comment for the
			// "prompt over-reports Ceph" failure mode this avoids.
			sp, err := h.generateStoragePlan(nodeCtx,
				host,
				privateKey,
				hetznerConfig.BareMetal.InstallImage.VG0.Size,
				hetznerConfig.BareMetal.ZFS.Size,
			)
			if err != nil {
				return fmt.Errorf("node-group %s server %s: %w", nodeGroup.Name, host.ServerID, err)
			}
			storagePlans[i] = sp

			host.WWNs = collectAndSortWWNs(sp.OS)
		}

		if err := storageplan.CheckStoragePlansAlike(storagePlans); err != nil {
			return fmt.Errorf("node-group %s storage plans aren't alike: %w", nodeGroup.Name, err)
		}

		allStoragePlans[nodeGroup.Name] = storagePlans
		nodeGroup.StoragePlan = *storagePlans[0]
	}

	allStoragePlans.GetApproval(ctx)

	// Persist the operator-approved ZFS pool size into general.yaml so
	// the file ends up self-describing — a subsequent re-run sees the
	// explicit `cloud.hetzner.bareMetal.zfs.size: <N>` rather than
	// relying on the struct's default:"220" tag to refill it. Idempotent:
	// if the key is already there, it's overwritten with the same value.
	if err := persistApprovedZFSSize(ctx, hetznerConfig.BareMetal.ZFS.Size); err != nil {
		return fmt.Errorf("persisting approved ZFS pool size to general.yaml: %w", err)
	}
	return nil
}

// collectAndSortWWNs extracts each disk's WWN into a freshly-allocated
// slice and returns it sorted lexicographically.
//
// Why sort: the source slice (sp.OS) is filled in the order the
// allocator picked OS disks, which derives from lsblk's enumeration
// order on the node. Linux re-enumerates PCIe / NVMe devices on
// every boot, so the same two physical disks can come back as
// (nvme0n1, nvme1n1) on one boot and (nvme1n1, nvme0n1) on the next.
// Persisting WWNs in that volatile order made the rendered chart
// values churn on every kubeaid-cli re-run — the operator saw a
// noisy `- foo / + bar` diff against kubeaid-config that was a pure
// reordering with no semantic change. CAPH reads the list as a set
// (the WWNs are joined with "|" inside the RAID block), so sorting
// here is free and gives byte-stable renders across runs.
func collectAndSortWWNs(disks []*storageplan.Disk) []string {
	out := make([]string, 0, len(disks))
	for _, d := range disks {
		out = append(out, d.WWN)
	}
	sort.Strings(out)
	return out
}

// persistApprovedZFSSize writes size into general.yaml at all three
// positions kubeaid-cli + the chart consult: top-level
// cloud.hetzner.bareMetal.zfs.size (read by storage-plan generation),
// cloud.hetzner.controlPlane.bareMetal.zfs.size (read by the chart's
// KubeadmControlPlane), and every
// cloud.hetzner.nodeGroups.bareMetal[i].zfs.size (read by the chart's
// per-host KubeadmConfig). Keeping the three in lock-step at write
// time is what makes "the operator approved one number" actually true
// across the whole bootstrap; previously a hand-edit at any one
// position would silently diverge from the others.
//
// yaml.Node tree manipulation (rather than re-marshalling the parsed
// struct) preserves the file's existing comments — most notably the
// `# Robot main IP: …` annotations alongside each bareMetalHost — and
// the operator's key ordering.
//
// Called once after `allStoragePlans.GetApproval(ctx)` returns "yes",
// so the value is whatever the operator confirmed in the storage-plan
// box (the same value the struct carried via default:"220", or any
// explicit override the operator pre-set in general.yaml).
func persistApprovedZFSSize(ctx context.Context, size int) error {
	path := config.GetGeneralConfigFilePath()
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s is not a YAML mapping", path)
	}

	hetzner := findYAMLChild(root.Content[0], "cloud", "hetzner")
	if hetzner == nil {
		return fmt.Errorf("cloud.hetzner not found in %s", path)
	}

	// Top-level bareMetal — required (this is what the storage planner reads).
	topBareMetal := findYAMLChild(hetzner, "bareMetal")
	if topBareMetal == nil {
		return fmt.Errorf("cloud.hetzner.bareMetal not found in %s", path)
	}
	upsertYAMLIntField(topBareMetal, "zfs", "size", size)

	// controlPlane.bareMetal — only present when the CP runs on bare metal.
	// Absent for hcloud-CP modes; skip silently rather than erroring.
	if cpBareMetal := findYAMLChild(hetzner, "controlPlane", "bareMetal"); cpBareMetal != nil {
		upsertYAMLIntField(cpBareMetal, "zfs", "size", size)
	}

	// nodeGroups.bareMetal[] — every entry that exists, regardless of
	// mode. hcloud-only configs have no bareMetal sequence here, so the
	// findYAMLChild result is nil and the range below is a no-op.
	if ngBareMetal := findYAMLChild(hetzner, "nodeGroups", "bareMetal"); ngBareMetal != nil &&
		ngBareMetal.Kind == yaml.SequenceNode {
		for _, entry := range ngBareMetal.Content {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			upsertYAMLIntField(entry, "zfs", "size", size)
		}
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	slog.InfoContext(ctx, "Persisted operator-approved ZFS pool size to general.yaml",
		slog.Int("size-gb", size), slog.String("path", path))
	return nil
}

// findYAMLChild walks a yaml.MappingNode following the given key path
// and returns the leaf value node, or nil if any hop is missing or
// the wrong kind.
func findYAMLChild(node *yaml.Node, keys ...string) *yaml.Node {
	current := node
	for _, key := range keys {
		if current == nil || current.Kind != yaml.MappingNode {
			return nil
		}
		next := (*yaml.Node)(nil)
		for i := 0; i+1 < len(current.Content); i += 2 {
			if current.Content[i].Value == key {
				next = current.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

// upsertYAMLIntField sets parent.outerKey.innerKey = value, creating
// the outer mapping and the inner scalar if either is absent. Used so
// persistApprovedZFSSize can write `zfs: size: <N>` whether or not the
// `zfs:` block already exists under bareMetal.
func upsertYAMLIntField(parent *yaml.Node, outerKey, innerKey string, value int) {
	outer := (*yaml.Node)(nil)
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == outerKey {
			outer = parent.Content[i+1]
			break
		}
	}
	if outer == nil {
		outer = &yaml.Node{Kind: yaml.MappingNode}
		parent.Content = append(parent.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: outerKey},
			outer,
		)
	}

	valueStr := strconv.Itoa(value)
	for i := 0; i+1 < len(outer.Content); i += 2 {
		if outer.Content[i].Value == innerKey {
			outer.Content[i+1].Kind = yaml.ScalarNode
			outer.Content[i+1].Tag = "!!int"
			outer.Content[i+1].Value = valueStr
			return
		}
	}
	outer.Content = append(outer.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: innerKey},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: valueStr},
	)
}

func (h *Hetzner) generateStoragePlan(ctx context.Context,
	host *config.HetznerBareMetalHost,
	privateKey string,
	osSize,
	zfsPoolSize int,
) (*storageplan.StoragePlan, error) {
	address, err := h.getHetznerBareMetalServerIP(host.ServerID)
	if err != nil {
		return nil, fmt.Errorf("getting server IP: %w", err)
	}

	// Reuse the SSH connection isHBMSReachable opened during the
	// install-wait phase. Cache-hit ⇒ no new TCP+KEX, no new yubikey
	// touch. If the cache somehow missed (operator skipped the OS
	// install via idempotency, host SSH-reachable from the start),
	// the pool opens fresh and tells the operator with a touch hint;
	// the same connection then services every storage-plan command
	// (lsblk + ethtool + …) over its single authenticated channel.
	connection, err := h.sshPool.getOrOpen(ctx, address, privateKey,
		fmt.Sprintf("scan disks on Hetzner bare-metal server at %s", address),
	)
	if err != nil {
		return nil, fmt.Errorf("opening SSH connection to %s: %w", address, err)
	}

	commandExecutor := commandexecutor.NewSSHCommandExecutor(connection)

	sp, err := storageplanner.GenerateStoragePlan(ctx,
		host.ServerID,
		commandExecutor,
		osSize,
		zfsPoolSize,
	)
	if err != nil {
		return nil, fmt.Errorf("generating storage plan: %w", err)
	}

	return sp, nil
}
