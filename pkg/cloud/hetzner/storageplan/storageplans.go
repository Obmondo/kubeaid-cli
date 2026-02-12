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

func (s StoragePlans) PrettyPrint() {
	// Construct node-group tree.
	// NOTE : By x-tree, we indicate a tree with xs as children.
	t := tree.Root(".").Enumerator(tree.RoundedEnumerator)
	for nodeGroupName, storagePlans := range s {

		// Construct node-tree for each node-group.
		nodeTree := tree.Root(nodeGroupName)
		for _, storagePlan := range storagePlans {

			// Construct disk-tree for each node.
			diskTree := tree.Root(storagePlan.ServerID)
			for _, disk := range storagePlan.Disks {

				// Construct allocation-tree for each disk.

				diskAllocationTreeLabel := disk.Name
				if disk.Unallocated() > 0 {
					diskAllocationTreeLabel += fmt.Sprintf(" (%d GB unallocated)", disk.Unallocated())
				}

				diskAllocationTree := tree.Root(diskAllocationTreeLabel)

				if disk.Allocations.OS > 0 {
					diskAllocationTree = diskAllocationTree.Child(
						fmt.Sprintf("OS   : %d GB", disk.Allocations.OS),
					)
				}
				if disk.Allocations.ZFS > 0 {
					diskAllocationTree = diskAllocationTree.Child(
						fmt.Sprintf("ZFS  : %d GB", disk.Allocations.ZFS),
					)
				}
				if disk.Allocations.CEPH > 0 {
					diskAllocationTree = diskAllocationTree.Child(
						fmt.Sprintf("CEPH : %d GB", disk.Allocations.CEPH),
					)
				}

				diskTree = diskTree.Child(diskAllocationTree)
			}

			nodeTree.Child(diskTree)
		}

		t = t.Child(nodeTree)
	}

	fmt.Println(t.String())
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
