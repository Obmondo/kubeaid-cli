// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner/storageplan"
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
	disks, err := getDisks(ctx, commandExecutor)
	if err != nil {
		return nil, err
	}

	return allocateStoragePlan(serverID, disks, osSize, zfsPoolSize)
}

func allocateStoragePlan(
	serverID string,
	disks []*storageplan.Disk,
	osSize,
	zfsPoolSize int,
) (*storageplan.StoragePlan, error) {
	s := &storageplan.StoragePlan{ServerID: serverID, Disks: disks}

	osDisks, err := allocateTwoDisks(disks, osSize, func(d *storageplan.Disk) int {
		return d.PriorityScores.OS
	}, func(d *storageplan.Disk) {
		d.Allocations.OS += osSize
	})
	if err != nil {
		return nil, errors.New("couldn't find 2 disks suitable for OS installation")
	}
	s.OS = osDisks

	zfsDisks, err := allocateTwoDisks(disks, zfsPoolSize, func(d *storageplan.Disk) int {
		return d.PriorityScores.ZFS
	}, func(d *storageplan.Disk) {
		d.Allocations.ZFS += zfsPoolSize
	})
	if err != nil {
		return nil, errors.New("couldn't find 2 disks suitable for ZFS pool installation")
	}
	s.ZFS = zfsDisks

	for _, disk := range disks {
		unallocated := disk.Unallocated()
		if unallocated < constants.CEPHNodeMinSize {
			continue
		}

		disk.Allocations.CEPH = unallocated
		s.CEPH = append(s.CEPH, disk)
	}

	return s, nil
}

func allocateTwoDisks(
	disks []*storageplan.Disk,
	allocationSize int,
	priorityScore func(*storageplan.Disk) int,
	allocate func(*storageplan.Disk),
) ([]*storageplan.Disk, error) {
	slices.SortFunc(disks, func(a *storageplan.Disk, b *storageplan.Disk) int {
		priorityScoreDifference := priorityScore(b) - priorityScore(a)
		if priorityScoreDifference != 0 {
			return priorityScoreDifference
		}

		return strings.Compare(a.Name, b.Name)
	})

	targetDisks := []*storageplan.Disk{}
	for _, disk := range disks {
		if (len(targetDisks) >= 2) || (disk.Unallocated() < allocationSize) {
			continue
		}

		allocate(disk)
		targetDisks = append(targetDisks, disk)
	}
	if len(targetDisks) != 2 {
		return nil, errors.New("couldn't find 2 suitable disks")
	}

	return targetDisks, nil
}

type (
	LSBLKOutput struct {
		BlockDevices []LSBLKOutputRow `json:"blockdevices"`
	}

	// REFER: util-linux lsblk source.
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
func getDisks(
	ctx context.Context,
	commandExecutor commandexecutor.CommandExecutor,
) ([]*storageplan.Disk, error) {
	slog.InfoContext(ctx, "Getting the server's disks")

	// Determine whether the server has a high speed NIC (bandwidth >= 5 GBPS) attached or not.

	stdout, err := commandExecutor.Execute(ctx, `
    for i in /sys/class/net/*;
      do [ -e "$i/device" ] && cat "$i/speed" 2>/dev/null;
    done || true
  `)
	if err != nil {
		return nil, fmt.Errorf("listing NIC speeds: %w", err)
	}

	maxNICSpeed := 0
	for nicSpeed := range strings.FieldsSeq(stdout) {
		parsedNICSpeed, err := strconv.Atoi(nicSpeed)
		if err != nil {
			return nil, fmt.Errorf("parsing NIC speed %q: %w", nicSpeed, err)
		}

		maxNICSpeed = max(maxNICSpeed, parsedNICSpeed)
	}

	// List hardware disks, using lsblk.

	stdout, err = commandExecutor.Execute(ctx, "lsblk -dn -o NAME,TRAN,ROTA,WWN,SIZE,PTTYPE -J --bytes")
	if err != nil {
		return nil, fmt.Errorf("listing hardware disks: %w", err)
	}

	var lsblkOutput LSBLKOutput
	if err := json.Unmarshal([]byte(stdout), &lsblkOutput); err != nil {
		return nil, fmt.Errorf("unmarshalling lsblk output: %w", err)
	}

	// Filter out rows which correspond to unknown disk types.
	lsblkOutput.BlockDevices = slices.DeleteFunc(lsblkOutput.BlockDevices, func(row LSBLKOutputRow) bool {
		return row.GetDiskType() == constants.DiskTypeUnknown
	})

	disks := make([]*storageplan.Disk, len(lsblkOutput.BlockDevices))
	for i, row := range lsblkOutput.BlockDevices {
		if len(row.PartitionTableType) == 0 {
			return nil, fmt.Errorf("empty partition table type for disk %q", row.Name)
		}

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
	return disks, nil
}
