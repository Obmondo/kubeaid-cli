// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import "github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"

type (
	Disk struct {
		Name,
		WWN,
		Type,
		PartitionTableType string

		Size int // GB.

		// Whether the server to which this disk is attached, has a NIC (Network Interface Card)
		// with speed >= 5 GBPS.
		// If yes, and this disk is an SSD / NVMe, then the priority score for CEPH installation
		// increases : since the CEPH OSDs can leverage the increased network bandwidth for higher
		// disk IOPS.
		WithHighSpeedNIC bool

		PriorityScores PriorityScores

		Allocations struct {
			OS,
			ZFS,
			CEPH int // GB.
		}
	}

	PriorityScores struct {
		OS,
		ZFS int
	}
)

// Returns the total amount of storage space allocated until now.
func (d *Disk) Allocated() int {
	allocations := d.Allocations
	return (allocations.OS + allocations.ZFS + allocations.CEPH)
}

// Returns the amount of unallocated storage.
func (d *Disk) Unallocated() int {
	return (d.Size - d.Allocated())
}

// Assigns priority scores to the disk, for OS and ZFS installations.
func (d *Disk) AssignPriorityScores() {
	d.PriorityScores = PriorityScores{}

	d.PriorityScores.OS = (func() int {
		switch d.Type {
		case constants.DiskTypeHDD:
			return 3

		case constants.DiskTypeSSD:
			return 2

		case constants.DiskTypeNVMe:
			return 1

		default:
			return 0
		}
	})()

	d.PriorityScores.ZFS = (func() int {
		switch {
		// When we have a high speed NIC attached to the server, ZFS should run on the slower disks.
		// So Rook CEPH ends up on the faster disks, taking advantage of the higher network bandwidth.
		case d.WithHighSpeedNIC && (d.Type == constants.DiskTypeNVMe):
			return 1
		case d.WithHighSpeedNIC && (d.Type == constants.DiskTypeSSD):
			return 2

		// Otherwise, ZFS should be on the faster disks.
		case d.Type == constants.DiskTypeHDD:
			return 3
		case d.Type == constants.DiskTypeSSD:
			return 4
		case d.Type == constants.DiskTypeNVMe:
			return 5

		default:
			return 0
		}
	})()
}
