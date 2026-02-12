// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

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
