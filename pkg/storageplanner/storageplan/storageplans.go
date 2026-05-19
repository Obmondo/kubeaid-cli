// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type (
	NodeGroupName = string

	StoragePlans map[NodeGroupName][]*StoragePlan
)

// PrettyPrint renders the storage plans as a tree. Since
// AreStoragePlansAlike enforces an identical layout across every
// server in a node group, each group renders two summary lines
// instead of a per-disk subtree: the disk composition per server
// (collapsed by type / size — "2 × NVMe 1 TB"), and the ZFS pool
// sub-volume breakdown (mount points + sizes + OpenEBS overflow).
//
// The per-disk subtree we used to print here got repetitive — the
// two disks in a mirror-pair always carry the same allocation by
// definition, so showing each with "OS: 50 GB / ZFS: 220 GB"
// duplicated rows without adding signal. The compact form keeps
// the hardware + mount facts the operator needs to verify but cuts
// the line count in half for the typical 2-disk setup.
func (s StoragePlans) PrettyPrint() {
	t := tree.Root(".").Enumerator(tree.RoundedEnumerator)
	for nodeGroupName, storagePlans := range s {
		if len(storagePlans) == 0 {
			continue
		}

		// All N plans are alike (enforced by AreStoragePlansAlike);
		// the first one is the canonical layout for the group.
		first := storagePlans[0]
		header := nodeGroupName + " " + serverIDsHeader(storagePlans)
		groupTree := tree.Root(header)
		groupTree.Child("Disks per server: " + formatDiskComposition(first.Disks))
		groupTree.Child(zfsPoolSubTree(first.Disks))

		t = t.Child(groupTree)
	}

	fmt.Println(t.String())
}

// formatDiskComposition collapses N disks of the same Type + Size
// into "N × Type Size", joins distinct types with " + ":
//
//	[NVMe 1TB, NVMe 1TB]          → "2 × NVMe 1 TB"
//	[NVMe 1TB, HDD 4TB]           → "NVMe 1 TB + HDD 4 TB"
//	[HDD 2TB]                     → "HDD 2 TB"
//	[NVMe 1TB, NVMe 1TB, HDD 4TB] → "2 × NVMe 1 TB + HDD 4 TB"
//
// Order in the output preserves first-encountered order of the
// (Type, Size) tuple across the input slice — matches lsblk's natural
// reporting order so the operator sees the disks in the same shape
// they'd see in the Hetzner Robot UI.
func formatDiskComposition(disks []*Disk) string {
	type key struct {
		Type string
		Size int
	}
	counts := map[key]int{}
	order := make([]key, 0, len(disks))
	for _, d := range disks {
		k := key{Type: d.Type, Size: d.Size}
		if _, seen := counts[k]; !seen {
			order = append(order, k)
		}
		counts[k]++
	}

	parts := make([]string, 0, len(order))
	for _, k := range order {
		part := fmt.Sprintf("%s %s", k.Type, formatDiskSize(k.Size))
		if counts[k] > 1 {
			part = fmt.Sprintf("%d × %s", counts[k], part)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " + ")
}

// formatDiskShorthand renders a no-sizes variant — "2 NVMe" or
// "NVMe + HDD" — used in the ZFS pool header where the disk sizes
// have already been shown one line above. The pool header reads
// naturally as "mirror across 2 NVMe" rather than "mirror across 2
// × NVMe 1 TB".
func formatDiskShorthand(disks []*Disk) string {
	counts := map[string]int{}
	order := make([]string, 0, len(disks))
	for _, d := range disks {
		if _, seen := counts[d.Type]; !seen {
			order = append(order, d.Type)
		}
		counts[d.Type]++
	}
	parts := make([]string, 0, len(order))
	for _, t := range order {
		if counts[t] > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[t], t))
			continue
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " + ")
}

// formatDiskSize renders a disk size in GB as a human label:
// integer GB up to 999 ("999 GB"), then TB above that ("1 TB",
// "1.5 TB", "1.7 TB"). Sub-TB sizes stay in GB because operators
// remember Hetzner offerings in GB ("Hetzner AX42 ships 1 TB", "AX52
// ships 1 TB or 1.92 TB") and a 100 GB OS-only NVMe should read as
// "100 GB", not "0.1 TB".
func formatDiskSize(gb int) string {
	if gb < 1000 {
		return fmt.Sprintf("%d GB", gb)
	}
	tb := float64(gb) / 1000.0
	// Round to 1 decimal place; trim a trailing ".0" so "1000 GB" → "1 TB".
	rounded := fmt.Sprintf("%.1f", tb)
	rounded = strings.TrimSuffix(rounded, ".0")
	return rounded + " TB"
}

// zfsPoolSubTree renders the ZFS pool's sub-volume breakdown.
// Header line: pool name, RAID mode (always mirror for the two-disk
// case kubeaid-cli supports today), and the OS-side facts (ext4 +
// RAID-1 via mdadm) so the operator gets the full filesystem picture
// without a separate node. Body: the three fixed-size sub-volumes
// (containerd / pod-logs / pod-ephemeral) plus the OpenEBS
// LocalPV-backed remainder. Mount-point column is left-padded to a
// fixed width so the right-aligned size column stays scannable.
func zfsPoolSubTree(disks []*Disk) *tree.Tree {
	zfsSize := 0
	for _, d := range disks {
		zfsSize += d.Allocations.ZFS
	}
	// In a mirror, usable space = single-disk allocation (writes go
	// to both copies). Summing across disks double-counts; divide by
	// the disk count to get the operator-visible pool capacity.
	if len(disks) > 0 {
		zfsSize /= len(disks)
	}

	header := fmt.Sprintf(
		`ZFS pool "primary" — mirror across %s (OS: ext4 on RAID-1)`,
		formatDiskShorthand(disks),
	)
	t := tree.Root(header)

	type row struct {
		label string
		size  int
	}
	rows := []row{
		{"/var/lib/containerd", constants.ZFSVolumeSizeContainerImages},
		{"/var/log/pods", constants.ZFSVolumeSizePodLogs},
		{"/var/lib/kubelet/pods", constants.ZFSVolumeSizePodEphemeralVolumes},
	}
	reserved := constants.ZFSVolumeSizeContainerImages +
		constants.ZFSVolumeSizePodLogs +
		constants.ZFSVolumeSizePodEphemeralVolumes
	openebs := zfsSize - reserved
	if openebs < 0 {
		openebs = 0
	}
	rows = append(rows, row{"OpenEBS ZFS LocalPV", openebs})

	// Widest label across all rows → left-pad the rest so the
	// "size" column aligns visually.
	labelWidth := 0
	for _, r := range rows {
		if l := len(r.label); l > labelWidth {
			labelWidth = l
		}
	}
	for i, r := range rows {
		size := fmt.Sprintf("%3d GB", r.size)
		if i == len(rows)-1 { // OpenEBS row gets a "free" suffix.
			size += " free"
		}
		t.Child(fmt.Sprintf("%-*s : %s", labelWidth, r.label, size))
	}
	return t
}

// serverIDsHeader renders a "(N servers: ID1, ID2, ID3)" suffix for
// the node-group tree header. Single-server groups get "(server ID)";
// large groups (>4) get a truncated list so a 20-node worker pool
// doesn't blow out the line width.
func serverIDsHeader(storagePlans []*StoragePlan) string {
	if len(storagePlans) == 1 {
		return fmt.Sprintf("(server %s)", storagePlans[0].ServerID)
	}
	ids := make([]string, 0, len(storagePlans))
	for _, sp := range storagePlans {
		ids = append(ids, sp.ServerID)
	}
	const previewCount = 4
	if len(ids) > previewCount {
		shown := strings.Join(ids[:previewCount], ", ")
		return fmt.Sprintf("(%d servers: %s, …+%d more)", len(ids), shown, len(ids)-previewCount)
	}
	return fmt.Sprintf("(%d servers: %s)", len(ids), strings.Join(ids, ", "))
}

func (s *StoragePlans) GetApproval(ctx context.Context) {
	// Pretty print the storage plan.
	s.PrettyPrint()

	// Request for approval.
	for {
		fmt.Print(lipgloss.NewStyle().Render("Proceed with the above storage plans? (yes/no): "))

		response, err := bufio.NewReader(os.Stdin).ReadString('\n')
		assert.AssertErrNil(ctx, err, "Failed reading user input")

		switch strings.TrimSpace(strings.ToLower(response)) {
		case "yes":
			return

		case "no":
			slog.ErrorContext(ctx, "Storage plans not approved")
			os.Exit(1)
		}
	}
}

/*
By alikeness, I mean, the 2 disks across which the ZFS pool will be running, must be the same
across all the nodes in the node-group. This makes the command to create a ZFS pool to be the same
across the nodes, for e.g. :

	zpool create primary mirror /dev/nvme0n1 /dev/nvme1n1

For all the nodes in a node-group, we have a single KubeadmControlPlane / KubeadmConfig resource.
And the ZFS pool creation command goes in the postKubeadm section of that resource. So, it must be
same for all the nodes.
*/
func AreStoragePlansAlike(storagePlans []*StoragePlan) bool {
	var referenceDisks []*Disk
	for i, storagePlan := range storagePlans {
		if i == 0 {
			referenceDisks = storagePlan.ZFS
			continue
		}

		if len(storagePlan.ZFS) != len(referenceDisks) {
			return false
		}
		for j, disk := range storagePlan.ZFS {
			if referenceDisks[j].Name != disk.Name {
				return false
			}
		}
	}

	return true
}
