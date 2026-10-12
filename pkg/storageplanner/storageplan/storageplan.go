// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package storageplan

import (
	"context"
	"embed"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/lipgloss/tree"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/storagetypes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/commandexecutor"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// StoragePlan is defined in pkg/storagetypes so pkg/config can embed it
// without pulling this package's executor, templates and tree printing.
// Aliased here; the behaviour below stays.
type (
	StoragePlan    = storagetypes.StoragePlan
	Disk           = storagetypes.Disk
	PriorityScores = storagetypes.PriorityScores
)

// NewDisk is re-exported so callers of this package are unchanged.
var NewDisk = storagetypes.NewDisk

type StoragePlanExecutorTemplateValues struct {
	StoragePlan *StoragePlan

	// EtcdDataset, when set, gives etcd its own dataset on the ZFS pool,
	// mounted at EtcdDataDirectory with EtcdDatasetReservation GB reserved.
	EtcdDataset            bool
	EtcdDataDirectory      string
	EtcdDatasetReservation int
}

// ExecuteOptions tunes what Execute sets up beyond the storage plan itself.
type ExecuteOptions struct {
	// EtcdDataset puts etcd's data directory on the ZFS pool instead of the OS
	// volume. The OS volume lands on the server's slowest disk pair (see
	// Disk.AssignPriorityScores) and shares those disks with a Rook Ceph OSD,
	// which is too slow for etcd's WAL fsync. Only for control-plane nodes.
	EtcdDataset bool
}

// Returns the UI tree, which can be used to pretty print the storage-plan.
//
// Used by the standalone kubeaid-storagectl tool: one plan per print,
// per-disk allocation visible because that tool's job is to inspect
// individual servers. The bootstrap-time group-level rendering lives
// in StoragePlans.PrettyPrint and uses the compact composition + ZFS
// sub-volume summary instead.
func getUITree(s *StoragePlan) *tree.Tree {
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

func PrettyPrint(s *StoragePlan) {
	fmt.Println(getUITree(s).String())
}

//go:embed templates/*
var templates embed.FS

// Executes the storage plan, by running necessary shell commands.
func Execute(ctx context.Context,
	s *StoragePlan,
	commandExecutor commandexecutor.CommandExecutor,
	options ExecuteOptions,
) {
	// Generate the shell commands to execute the storage plan.

	storagePlanExecutorTemplateValues := &StoragePlanExecutorTemplateValues{
		StoragePlan: s,

		EtcdDataset:            options.EtcdDataset,
		EtcdDataDirectory:      constants.EtcdDataDirectory,
		EtcdDatasetReservation: constants.ZFSEtcdDatasetReservation,
	}

	storagePlanExecutorAsBytes := templateUtils.ParseAndExecuteTemplate(ctx,
		&templates, constants.TemplateNameStoragePlanExecutor, storagePlanExecutorTemplateValues)

	// Run those shell commands.
	commandExecutor.MustExecute(ctx, string(storagePlanExecutorAsBytes))
	slog.InfoContext(ctx, "Executed storage plan")
}
