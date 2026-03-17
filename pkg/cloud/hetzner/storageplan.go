// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8c.io/kubeone/pkg/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner/storageplan"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner/storageplan/storageplanner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (h *Hetzner) GenerateStoragePlans(ctx context.Context, hetznerConfig *config.HetznerConfig) {
	allStoragePlans := make(storageplan.StoragePlans)

	privateKey := hetznerConfig.SSHKeyPair.PrivateKey

	/*
		When the control-plane is in Hetzner bare-metal, we :

		(1) Generate storage plan for the control-plane nodes.

		(2) Check whether the storage plans are alike or not.

		    By alikeness, I mean, on each node, the 2 disks across which the ZFS pool will be running,
		    must be same. This makes the command to create a ZFS pool to be same across the nodes,
		    for e.g. :

		                  zpool create primary mirror /dev/nvme0n1 /dev/nvme1n1

		    NOTE : For all the control-plane nodes, we have a single KubeadmControlPlane resource.
		           And the ZFS pool creation command goes in the postKubeadm section of that resource.
		           So, it must be same for all the nodes.

		(3) Pretty print the storage plan for each node, and get approval from the user.
	*/
	if config.ControlPlaneInHetznerBareMetal() {
		nodeGroupCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", "control-plane"),
		})

		storagePlans := make([]*storageplan.StoragePlan,
			len(hetznerConfig.ControlPlane.BareMetal.BareMetalHosts),
		)

		for i, host := range hetznerConfig.ControlPlane.BareMetal.BareMetalHosts {
			nodeCtx := logger.AppendSlogAttributesToCtx(nodeGroupCtx, []slog.Attr{
				slog.String("server-id", host.ServerID),
			})

			disks := h.getServerDisks(nodeCtx, host.ServerID, privateKey)

			storagePlan, err := storageplanner.NewStoragePlan(nodeCtx, host.ServerID,
				hetznerConfig.BareMetal.InstallImage.VG0.RootVolumeSize,
				hetznerConfig.ControlPlane.BareMetal.ZFS,
				disks,
			)
			assert.AssertErrNil(nodeCtx, err, "Failed generating storage plan")

			storagePlans[i] = storagePlan

			// Store WWNs of the 2 disks across which the OS will be installed,
			// into the BareMetalHostConfig.
			host.WWNs = []string{}
			for _, disk := range storagePlan.OS {
				host.WWNs = append(host.WWNs, disk.WWN)
			}
		}

		// Check alikeness of storage plans.
		storagePlansAlike := storageplan.AreStoragePlansAlike(storagePlans)
		assert.Assert(nodeGroupCtx, storagePlansAlike, "Storage plans aren't alike")

		allStoragePlans["control-plane"] = storagePlans

		hetznerConfig.ControlPlane.BareMetal.StoragePlan = *storagePlans[0]
	}

	// We do the similar for each Hetzner bare-metal node-group.
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		nodeGroupCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", nodeGroup.Name),
		})

		storagePlans := make([]*storageplan.StoragePlan, len(nodeGroup.BareMetalHosts))

		for i, host := range nodeGroup.BareMetalHosts {
			nodeCtx := logger.AppendSlogAttributesToCtx(nodeGroupCtx, []slog.Attr{
				slog.String("server-id", host.ServerID),
			})

			disks := h.getServerDisks(nodeCtx, host.ServerID, privateKey)

			storagePlan, err := storageplanner.NewStoragePlan(nodeCtx, host.ServerID,
				hetznerConfig.BareMetal.InstallImage.VG0.RootVolumeSize,
				nodeGroup.ZFS,
				disks,
			)
			assert.AssertErrNil(nodeCtx, err, "Failed generating storage plan")

			storagePlans[i] = storagePlan

			// Store WWNs of the 2 disks across which the OS will be installed,
			// into the BareMetalHostConfig.
			host.WWNs = []string{}
			for _, disk := range storagePlan.OS {
				host.WWNs = append(host.WWNs, disk.WWN)
			}
		}

		// Check alikeness of storage plans.
		storagePlansAlike := storageplan.AreStoragePlansAlike(storagePlans)
		assert.Assert(nodeGroupCtx, storagePlansAlike, "Storage plans aren't alike")

		allStoragePlans[nodeGroup.Name] = storagePlans

		nodeGroup.StoragePlan = *storagePlans[0]
	}

	allStoragePlans.GetApproval(ctx)
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

func (r *LSBLKOutputRow) GetDiskType() string {
	if r.RotationalDevice {
		return constants.DiskTypeHDD
	}

	switch r.TransportType {
	case TransportTypeSATA:
		return constants.DiskTypeSSD

	case TransportTypeNVMe:
		return constants.DiskTypeNVMe

	default:
		return constants.DiskTypeUnknown
	}
}

// Fetches disk details for the given Hetzner bare-metal server.
func (h *Hetzner) getServerDisks(ctx context.Context, id, privateKey string) []*storageplan.Disk {
	// Fetch the server's public IPv4 address.
	address := h.getServerIP(ctx, id)

	// Open an SSH connection to the server.
	connection, err := ssh.NewConnection(ssh.NewConnector(ctx), ssh.Opts{
		Context: ctx,

		Hostname:   address,
		Port:       22,
		Username:   "root",
		PrivateKey: privateKey,

		Timeout: time.Second * 10,
	})
	assert.AssertErrNil(ctx, err, "Failed opening SSH connection")
	defer connection.Close()

	// Determine whether the server has a high speed NIC (bandwidth >= 5 GBPS) attached or not.

	stdout, _, _, err := connection.Exec(`
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

	stdout, _, _, err = connection.Exec("lsblk -dn -o NAME,TRAN,ROTA,WWN,SIZE,PTTYPE -J --bytes")
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

type (
	GetServerResponseBody struct {
		Server Server `json:"server"`
	}

	Server struct {
		IP string `json:"server_ip"`
	}
)

// Fetches public IPv4 address of the Hetzner bare-metal server with the given ID.
func (h *Hetzner) getServerIP(ctx context.Context, id string) string {
	response, err := h.robotClient.R().Get("/server/" + id)
	assert.AssertErrNil(ctx, err, "Failed getting server details")
	assert.Assert(ctx,
		(response.StatusCode() == http.StatusOK),
		"Failed getting server details",
		slog.Any("response", response),
	)

	getServerResponseBody := GetServerResponseBody{}

	err = json.Unmarshal(response.Body(), &getServerResponseBody)
	assert.AssertErrNil(ctx, err, "Failed JSON unmarshalling GetServerResponseBody")

	return getServerResponseBody.Server.IP
}
