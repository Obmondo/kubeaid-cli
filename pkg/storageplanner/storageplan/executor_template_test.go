// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// TestStoragePlanExecutorTemplate_BlankDiskGuard renders the executor script
// and checks the blank-disk handling the storage planner relies on:
//   - every disk gets the "create a GPT label if the disk is blank" guard,
//     reading the live PTTYPE into a variable first so `set -e` aborts on an
//     lsblk failure instead of relabelling a non-blank disk;
//   - placeholder partitions 1-3 are created only on the non-OS disk (an OS
//     disk already carries its EFI/boot/root partitions and must keep them).
func TestStoragePlanExecutorTemplate_BlankDiskGuard(t *testing.T) {
	osDisk := &Disk{Name: "sda", Type: constants.DiskTypeHDD, PartitionTableType: "gpt"}
	osDisk.Allocations.OS = 80
	osDisk.Allocations.ZFS = 220

	dataDisk := &Disk{Name: "sdc", Type: constants.DiskTypeHDD, PartitionTableType: "gpt"}
	dataDisk.Allocations.CEPH = 14598

	plan := &StoragePlan{
		ServerID: "srv-blank",
		Disks:    []*Disk{osDisk, dataDisk},
		ZFS:      []*Disk{osDisk},
	}

	script := string(templateUtils.ParseAndExecuteTemplate(
		context.Background(),
		&templates,
		constants.TemplateNameStoragePlanExecutor,
		&StoragePlanExecutorTemplateValues{StoragePlan: plan},
	))

	// The guard renders for every disk: read the live PTTYPE into a variable,
	// then create a GPT label only when it is empty.
	for _, name := range []string{"sda", "sdc"} {
		assert.Contains(t, script, `pttype="$(lsblk -dn -o PTTYPE /dev/`+name+`)"`,
			"guard should read the live PTTYPE for %s", name)
		assert.Contains(t, script, `echo 'label: gpt' | sfdisk /dev/`+name+` --force --no-reread`,
			"guard should create a GPT label for %s", name)
	}

	// Placeholder partitions 1-3 are created only for the non-OS disk; the OS
	// disk must keep its existing EFI/boot/root partitions.
	assert.Contains(t, script, "sfdisk -N 1 /dev/sdc",
		"data disk should get a placeholder partition 1")
	assert.NotContains(t, script, "sfdisk -N 1 /dev/sda",
		"OS disk must not have partition 1 recreated")

	// The ZFS partition is sized from the allocation on the pool-backing disk.
	assert.Contains(t, script, `echo ",220G,L" | sfdisk -N 5 /dev/sda`,
		"OS/ZFS disk should get a 220G ZFS partition")
}
