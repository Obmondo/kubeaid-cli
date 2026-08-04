// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package storagetypes holds the bare-metal storage data types.
//
// Split out of pkg/storageplanner/storageplan so pkg/config can embed
// StoragePlan without inheriting that package's executor, shell templates
// and lipgloss tree printing — the same split as pkg/urlprotocol. Nothing
// here does I/O; keep it that way, or pkg/config stops being importable
// from a server.
package storagetypes

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
