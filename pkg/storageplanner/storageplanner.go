// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplanner

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner/storageplan"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
)

// Generates storage plan for the intended server.
func GenerateStoragePlan(ctx context.Context, serverID string,

	commandExecutor commandexecutor.CommandExecutor,

	osSize,
	zfsPoolSize int,
) (*storageplan.StoragePlan, error) {
	slog.InfoContext(ctx, "Generating storage plan")

	// Get the server's disks.
	disks := getDisks(ctx, commandExecutor)

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
			if (len(targetDisks) >= 2) || (unallocated < zfsPoolSize) {
				continue
			}

			disk.Allocations.ZFS += zfsPoolSize
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

type (
	LSBLKOutput struct {
		BlockDevices []LSBLKOutputRow `json:"blockdevices"`
	}

	// REFER : https://github.com/util-linux/util-linux/blob/4a4eb88f263bfffeee75cfcabcb6e364ef5900a3/misc-utils/lsblk.c#L174.
	LSBLKOutputRow struct {
		Name string `json:"name"`
		WWN  string `json:"wwn"`
		Size int    `json:"size"`

		RotationalDevice   bool   `json:"rota"`
		TransportType      string `json:"tran"`
		PartitionTableType string `json:"pttype"`
	}
)

const (
	TransportTypeSATA = "sata"
	TransportTypeNVMe = "nvme"
)

func (l *LSBLKOutputRow) GetDiskType() string {
	if l.RotationalDevice {
		return constants.DiskTypeHDD
	}

	switch l.TransportType {
	case TransportTypeSATA:
		return constants.DiskTypeSSD

	case TransportTypeNVMe:
		return constants.DiskTypeNVMe

	default:
		return constants.DiskTypeUnknown
	}
}

// Fetches disk details for the intended server, by leveraging the provided shell command executor.
func getDisks(ctx context.Context, commandExecutor commandexecutor.CommandExecutor) []*storageplan.Disk {
	slog.InfoContext(ctx, "Getting the server's disks")

	// Determine whether the server has a high speed NIC (bandwidth >= 5 GBPS) attached or not.

	stdout, err := commandExecutor.Execute(ctx, `
    for i in /sys/class/net/*;
      do [ -e "$i/device" ] && cat "$i/speed" 2>/dev/null;
    done || true
  `)
	assert.AssertErrNil(ctx, err, "Failed listing NIC speeds")

	maxNICSpeed := 0
	for nicSpeed := range strings.FieldsSeq(stdout) {
		parsedNICSpeed, err := strconv.Atoi(nicSpeed)
		assert.AssertErrNil(ctx, err, "Failed parsing NIC speed", slog.String("nic-speed", nicSpeed))

		maxNICSpeed = max(maxNICSpeed, parsedNICSpeed)
	}

	// List hardware disks, using lsblk.

	stdout, err = commandExecutor.Execute(ctx, "lsblk -dn -o NAME,TRAN,ROTA,WWN,SIZE,PTTYPE -J --bytes")
	assert.AssertErrNil(ctx, err, "Failed listing hardware disks")

	var lsblkOutput LSBLKOutput
	err = json.Unmarshal([]byte(stdout), &lsblkOutput)
	assert.AssertErrNil(ctx, err, "Failed unmarshalling lsblk output")

	// Filter out rows which correspond to unknown disk types.
	lsblkOutput.BlockDevices = slices.DeleteFunc(lsblkOutput.BlockDevices, func(row LSBLKOutputRow) bool {
		return row.GetDiskType() == constants.DiskTypeUnknown
	})

	disks := make([]*storageplan.Disk, len(lsblkOutput.BlockDevices))
	for i, row := range lsblkOutput.BlockDevices {
		assert.Assert(ctx, (len(row.PartitionTableType) > 0), "Empty partition table type",
			slog.String("disk", row.Name),
		)

		disks[i] = &storageplan.Disk{
			Name:               row.Name,
			WWN:                row.WWN,
			Type:               row.GetDiskType(),
			PartitionTableType: row.PartitionTableType,

			// 2G is kept aside for the boot and EFI partitions.
			Size: (row.Size / (1024 * 1024 * 1024)) - 2,

			WithHighSpeedNIC: (maxNICSpeed >= constants.HighSpeedNICThreshold),
		}
		disks[i].AssignPriorityScores()
	}
	return disks
}
