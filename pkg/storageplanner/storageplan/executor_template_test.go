// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package storageplan

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/commandexecutor/fake"
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

// etcdTestStoragePlan is a control-plane style server: OS on the HDD pair,
// ZFS pool on the SSD pair.
func etcdTestStoragePlan() *StoragePlan {
	// sda and sdb are the HDDs, sdc and sdd the SSDs.
	disks := make([]*Disk, 4)
	for i := range disks {
		disk := &Disk{Name: fmt.Sprintf("sd%c", 'a'+i), PartitionTableType: "gpt"}
		if i < 2 {
			disk.Type = constants.DiskTypeHDD
			disk.Allocations.OS = 80
		} else {
			disk.Type = constants.DiskTypeSSD
			disk.Allocations.ZFS = 220
		}
		disks[i] = disk
	}

	return &StoragePlan{
		ServerID: "srv-control-plane",
		Disks:    disks,
		OS:       disks[:2],
		ZFS:      disks[2:],
	}
}

// TestExecute_EtcdDataset checks the script Execute hands to the command
// executor when etcd's data directory is asked onto the ZFS pool.
func TestExecute_EtcdDataset(t *testing.T) {
	commandExecutor := fake.NewExecutor("")

	Execute(context.Background(), etcdTestStoragePlan(), commandExecutor, ExecuteOptions{
		EtcdDataset: true,
	})

	require.Len(t, commandExecutor.Commands, 1)
	script := commandExecutor.Commands[0]

	// The dataset is mounted where kubeadm points etcd, with space reserved
	// for it in the shared pool.
	createDataset := "zfs create \\\n" +
		"    -o mountpoint=/var/lib/etcd \\\n" +
		"    -o atime=off \\\n" +
		"    -o reservation=10G \\\n" +
		"    primary/etcd"
	require.Contains(t, script, createDataset)
	assert.Contains(t, script, "chmod 700 /var/lib/etcd")

	// It must not live inside the import-or-create branch: a re-provisioned
	// server imports its pool, takes the first arm, and still needs it. That
	// branch's closing `fi` is the only one at column 0 before the etcd block
	// (the per-disk ones are indented).
	importOrCreateEnd := strings.Index(script, "\nfi\n")
	require.NotEqual(t, -1, importOrCreateEnd, "import-or-create branch should be closed")
	assert.Greater(t, strings.Index(script, createDataset), importOrCreateEnd,
		"etcd dataset should be created after the import-or-create branch")

	// A leftover dataset from before the re-provision is dropped first, since
	// kubeadm refuses a non-empty etcd data directory...
	destroyDataset := strings.Index(script, "zfs destroy -r primary/etcd")
	require.NotEqual(t, -1, destroyDataset)
	assert.Less(t, destroyDataset, strings.Index(script, createDataset),
		"a leftover etcd dataset should be destroyed before the new one is created")

	// ...but never on a server where kubeadm already set etcd up.
	liveNodeGuard := strings.Index(script, "if [ -e /etc/kubernetes/manifests/etcd.yaml ]; then")
	require.NotEqual(t, -1, liveNodeGuard)
	assert.Less(t, liveNodeGuard, destroyDataset,
		"the live control-plane guard should come before the destroy")
}

// TestExecute_NoEtcdDatasetByDefault checks that worker nodes, which run
// without the option, get no etcd dataset.
func TestExecute_NoEtcdDatasetByDefault(t *testing.T) {
	commandExecutor := fake.NewExecutor("")

	Execute(context.Background(), etcdTestStoragePlan(), commandExecutor, ExecuteOptions{})

	require.Len(t, commandExecutor.Commands, 1)
	script := commandExecutor.Commands[0]

	assert.Contains(t, script, "zfs create primary/containerd",
		"the regular datasets should still be created")
	assert.NotContains(t, script, "etcd")
}
