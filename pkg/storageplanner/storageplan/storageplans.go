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
			nodeTree.Child(storagePlan.getUITree())
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

		for j, disk := range storagePlan.ZFS {
			if referenceDisks[j].Name != disk.Name {
				return false
			}
		}
	}

	return true
}
