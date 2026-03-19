// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"context"
	"embed"
	"fmt"

	"github.com/charmbracelet/lipgloss/tree"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
	templateUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

type StoragePlan struct {
	ServerID string

	Disks,

	// 2 disks across which the OS will get installed, with RAID 1 enabled.
	OS,

	// 2 disks across which the ZFS pool will be running, with RAIDZ-1 enabled.
	// We'll carve out ZFS volumes for : ContainerD's image store, pod logs and pod ephemeral volumes.
	// Remaining of the ZFS pool will be used by OpenEBS ZFS LocalPV provisioner CSI driver.
	ZFS,

	// Disks across which the CEPH cluster will be running.
	CEPH []*Disk
}

type StoragePlanExecutorTemplateValues struct {
	StoragePlan *StoragePlan
}

// Returns the UI tree, which can be used to pretty print the storage-plan.
func (s *StoragePlan) getUITree() *tree.Tree {
	t := tree.Root(s.ServerID)
	for _, disk := range s.Disks {

		// Construct allocation-tree for each disk.

		allocationTreeLabel := disk.Name
		if disk.Unallocated() > 0 {
			allocationTreeLabel += fmt.Sprintf(" (%d GB unallocated)", disk.Unallocated())
		}

		allocationTree := tree.Root(allocationTreeLabel)

		if disk.Allocations.OS > 0 {
			allocationTree = allocationTree.Child(
				fmt.Sprintf("OS   : %d GB", disk.Allocations.OS),
			)
		}
		if disk.Allocations.ZFS > 0 {
			allocationTree = allocationTree.Child(
				fmt.Sprintf("ZFS  : %d GB", disk.Allocations.ZFS),
			)
		}
		if disk.Allocations.CEPH > 0 {
			allocationTree = allocationTree.Child(
				fmt.Sprintf("CEPH : %d GB", disk.Allocations.CEPH),
			)
		}

		t = t.Child(allocationTree)
	}

	return t
}

func (s *StoragePlan) PrettyPrint() {
	fmt.Println(s.getUITree().String())
}

//go:embed templates/*
var templates embed.FS

// Executes the storage plan, by running necessary shell commands.
func (s *StoragePlan) Execute(ctx context.Context, commandExecutor commandexecutor.CommandExecutor) {
	// Generate the shell commands to execute the storage plan.

	storagePlanExecutorTemplateValues := &StoragePlanExecutorTemplateValues{StoragePlan: s}

	storagePlanExecutorAsBytes := templateUtils.ParseAndExecuteTemplate(ctx,
		&templates, constants.TemplateNameStoragePlanExecutor, storagePlanExecutorTemplateValues)

	// Run those shell commands.

	commandExecutor.MustExecute(ctx, string(storagePlanExecutorAsBytes))
}
