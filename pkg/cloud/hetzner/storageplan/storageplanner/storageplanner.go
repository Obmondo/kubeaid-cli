// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplanner

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner/storageplan"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// Responsible for generating the storage plan.
func NewStoragePlan(ctx context.Context, serverID string,

	osSize int,
	zfsConfig config.ZFSConfig,

	disks []*storageplan.Disk,
) (*storageplan.StoragePlan, error) {
	slog.InfoContext(ctx, "Generating storage plan")

	s := &storageplan.StoragePlan{ServerID: serverID, Disks: disks}

	// Allocate storage space for OS installation.
	{
		// Sort the disks slice based on priority score for OS installation, in descending order.
		// Disks with the same priority score, will be sorted alphabetically.
		slices.SortFunc(disks, func(a *storageplan.Disk, b *storageplan.Disk) int {
			priorityScoreDifference := b.PriorityScores.OS - a.PriorityScores.OS
			if priorityScoreDifference != 0 {
				return priorityScoreDifference
			}

			// Same priority score. So, sort alphabetically.
			return strings.Compare(a.Name, b.Name)
		})

		// Select first 2 disks with available storage space.

		targetDisks := []*storageplan.Disk{}
		for _, disk := range disks {
			if (len(targetDisks) >= 2) || (disk.Unallocated() < osSize) {
				continue
			}

			disk.Allocations.OS += osSize
			targetDisks = append(targetDisks, disk)
		}
		if len(targetDisks) != 2 {
			return nil, errors.New("couldn't find 2 disks suitable for OS installation")
		}
		s.OS = targetDisks
	}

	// Allocate storage space for ZFS pool.
	{
		// Sort the disks slice based on priority score for ZFS pool installation, in descending order.
		// Disks with the same priority score, will be sorted alphabetically.
		slices.SortFunc(disks, func(a *storageplan.Disk, b *storageplan.Disk) int {
			priorityScoreDifference := b.PriorityScores.ZFS - a.PriorityScores.ZFS
			if priorityScoreDifference != 0 {
				return priorityScoreDifference
			}

			// Same priority score. So, sort alphabetically.
			return strings.Compare(a.Name, b.Name)
		})

		// Select first 2 disks with available storage space.

		targetDisks := []*storageplan.Disk{}
		for _, disk := range disks {
			unallocated := disk.Unallocated()
			if (len(targetDisks) >= 2) || (unallocated < zfsConfig.Size) {
				continue
			}

			disk.Allocations.ZFS += zfsConfig.Size
			targetDisks = append(targetDisks, disk)
		}
		if len(targetDisks) != 2 {
			return nil, errors.New("couldn't find 2 disks suitable for ZFS pool installation")
		}
		s.ZFS = targetDisks
	}

	// Any disk having >= 50GB of storage space, will get allocated to the CEPH cluster.
	targetDisks := []*storageplan.Disk{}
	for _, disk := range disks {
		unallocated := disk.Unallocated()
		if unallocated < constants.CEPHNodeMinSize {
			continue
		}

		disk.Allocations.CEPH = unallocated
		targetDisks = append(targetDisks, disk)
	}
	s.CEPH = targetDisks

	return s, nil
}
