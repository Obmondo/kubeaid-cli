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
		return nil, fmt.Errorf(
			"kubeaid-cli requires 2 disks per server for the RAID-1 OS volume + ZFS mirror "+
				"but couldn't find 2 suitable for OS installation (%d GB each) — %s. "+
				"If this is a single-disk Hetzner SKU, order a 2-disk SKU or attach an extra drive via Robot",
			osSize, describeDisks(disks))
	}
	s.OS = osDisks

	zfsDisks, err := allocateTwoDisks(disks, zfsPoolSize, func(d *storageplan.Disk) int {
		return d.PriorityScores.ZFS
	}, func(d *storageplan.Disk) {
		d.Allocations.ZFS += zfsPoolSize
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't find 2 disks suitable for ZFS pool installation (%d GB each) — %s",
			zfsPoolSize, describeDisks(disks))
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

// describeDisks renders a one-line dump of every disk the planner
// considered, so the operator can see exactly what kubeaid-cli saw on
// the box when allocation failed. Format: "scanned N disk(s): <name>
// (<type>, <size> GB, <free> GB free); …" — short on purpose since
// it's appended to an already-wrapped error.
//
// Empty input ("scanned 0 disks") makes the lsblk-found-nothing case
// obvious (the typical cause being a freshly-installed server whose
// only block device is a virtio / loop / unknown-transport device
// that's filtered out as DiskTypeUnknown — see getDisks).
func describeDisks(disks []*storageplan.Disk) string {
	if len(disks) == 0 {
		return "scanned 0 disks (lsblk found nothing kubeaid-cli recognises as HDD / SSD / NVMe — check the host's block-device inventory)"
	}
	parts := make([]string, 0, len(disks))
	for _, d := range disks {
		parts = append(parts, fmt.Sprintf("%s (%s, %d GB, %d GB free)",
			d.Name, d.Type, d.Size, d.Unallocated()))
	}
	return fmt.Sprintf("scanned %d disk(s): %s", len(disks), strings.Join(parts, "; "))
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

	// Log the raw lsblk output BEFORE filtering. The
	// allocate-storage-plan error path otherwise hides single-disk
	// surprises (operator believes the box has 2 disks; kubeaid-cli
	// silently dropped one as DiskTypeUnknown because it had a
	// transport kubeaid-cli doesn't recognise yet, like usb / sas /
	// virtio under a vendor-customised SKU).
	if len(lsblkOutput.BlockDevices) == 0 {
		slog.WarnContext(ctx, "lsblk returned no block devices",
			slog.String("raw-output", stdout))
	}
	for _, row := range lsblkOutput.BlockDevices {
		slog.InfoContext(ctx, "lsblk row",
			slog.String("name", row.Name),
			slog.String("transport", row.TransportType),
			slog.Bool("rotational", row.RotationalDevice),
			slog.Int("size-bytes", row.Size),
			slog.String("partition-table-type", row.PartitionTableType),
			slog.String("classified-as", row.GetDiskType()),
		)
	}

	// Filter out rows which correspond to unknown disk types.
	// Capture which names get dropped so the error path can surface
	// them — a "second disk present in lsblk but classified Unknown"
	// case is otherwise indistinguishable from "second disk missing".
	var droppedNames []string
	lsblkOutput.BlockDevices = slices.DeleteFunc(lsblkOutput.BlockDevices, func(row LSBLKOutputRow) bool {
		drop := row.GetDiskType() == constants.DiskTypeUnknown
		if drop {
			droppedNames = append(droppedNames, fmt.Sprintf(
				"%s (tran=%q, rota=%t)", row.Name, row.TransportType, row.RotationalDevice))
		}
		return drop
	})
	if len(droppedNames) > 0 {
		slog.WarnContext(ctx, "Filtered disks with unrecognised type",
			slog.String("dropped", strings.Join(droppedNames, "; ")))
	}

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

	// Log the disk inventory at INFO so it shows up in the bootstrap
	// log without needing --debug. If allocateStoragePlan later fails
	// to find 2 suitable disks, the error message itself echoes the
	// same per-disk facts; logging here covers the success-but-still-
	// curious case ("what did kubeaid-cli actually pick from?") and
	// gives operators a quick before/after view across multiple bootstrap
	// attempts.
	slog.InfoContext(ctx, "Scanned server disks",
		slog.Int("count", len(disks)),
		slog.String("disks", describeDisksShort(disks)),
	)
	return disks, nil
}

// describeDisksShort is the slog-friendly variant of describeDisks —
// no leading "scanned N: " prefix (the count is already a separate
// log attribute).
func describeDisksShort(disks []*storageplan.Disk) string {
	parts := make([]string, 0, len(disks))
	for _, d := range disks {
		parts = append(parts, fmt.Sprintf("%s (%s, %d GB)", d.Name, d.Type, d.Size))
	}
	return strings.Join(parts, "; ")
}
