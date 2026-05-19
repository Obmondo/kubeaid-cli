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

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type (
	NodeGroupName = string

	StoragePlans map[NodeGroupName][]*StoragePlan
)

// PrettyPrint renders the storage plans as a tree. Since
// AreStoragePlansAlike enforces an identical layout across every
// server in a node group, the layout is shown ONCE per group with
// the server IDs listed in the group header — repeating the same
// disk allocations N times added noise without adding info.
func (s StoragePlans) PrettyPrint() {
	t := tree.Root(".").Enumerator(tree.RoundedEnumerator)
	for nodeGroupName, storagePlans := range s {
		if len(storagePlans) == 0 {
			continue
		}

		// All N plans are alike; show the first's disk layout under
		// a group label that names the servers it applies to.
		header := nodeGroupName + " " + serverIDsHeader(storagePlans)
		groupTree := tree.Root(header)
		for _, disk := range storagePlans[0].Disks {
			groupTree.Child(disk.getUITree())
		}

		t = t.Child(groupTree)
	}

	fmt.Println(t.String())
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
