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
//
// Used by the standalone kubeaid-storagectl tool (one plan per print);
// the group-level StoragePlans.PrettyPrint builds its tree from
// Disk.getUITree directly so it can collapse identical layouts across
// servers in a node-group into one display.
func (s *StoragePlan) getUITree() *tree.Tree {
	t := tree.Root(s.ServerID)
	for _, disk := range s.Disks {
		t = t.Child(disk.getUITree())
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
