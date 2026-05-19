// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"context"
	"embed"
	"fmt"
	"log/slog"

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

	// 2 disks across which the ZFS pool runs, as a ZFS mirror
	// (two-disk RAID-1 semantics — the executor template does
	// `zpool create primary mirror …`; raidz-1 needs ≥ 3 disks
	// anyway). We carve out ZFS volumes for ContainerD's image
	// store, pod logs, and pod ephemeral volumes; the remainder
	// backs the OpenEBS ZFS LocalPV provisioner CSI driver.
	ZFS,

	// Disks across which the CEPH cluster will be running.
	CEPH []*Disk
}

type StoragePlanExecutorTemplateValues struct {
	StoragePlan *StoragePlan
}

// Returns the UI tree, which can be used to pretty print the storage-plan.
//
// Used by the standalone kubeaid-storagectl tool: one plan per print,
// per-disk allocation visible because that tool's job is to inspect
// individual servers. The bootstrap-time group-level rendering lives
// in StoragePlans.PrettyPrint and uses the compact composition + ZFS
// sub-volume summary instead.
func (s *StoragePlan) getUITree() *tree.Tree {
	t := tree.Root(s.ServerID)
	for _, disk := range s.Disks {
		label := disk.Name
		if disk.Unallocated() > 0 {
			label += fmt.Sprintf(" (%d GB unallocated)", disk.Unallocated())
		}
		diskTree := tree.Root(label)
		if disk.Allocations.OS > 0 {
			diskTree = diskTree.Child(fmt.Sprintf("OS   : %d GB", disk.Allocations.OS))
		}
		if disk.Allocations.ZFS > 0 {
			diskTree = diskTree.Child(fmt.Sprintf("ZFS  : %d GB", disk.Allocations.ZFS))
		}
		if disk.Allocations.CEPH > 0 {
			diskTree = diskTree.Child(fmt.Sprintf("CEPH : %d GB", disk.Allocations.CEPH))
		}
		t = t.Child(diskTree)
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
	slog.InfoContext(ctx, "Executed storage plan")
}
