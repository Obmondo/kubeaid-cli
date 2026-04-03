// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"log/slog"
	"time"

	"k8c.io/kubeone/pkg/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner/storageplan"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
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

			storagePlan := h.generateStoragePlan(nodeCtx,

				host,
				privateKey,

				hetznerConfig.BareMetal.InstallImage.VG0.RootVolumeSize,
				hetznerConfig.BareMetal.ZFS.Size,
			)
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

	// We do the similar for each Hetzner Bare Metal node-group.
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		nodeGroupCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", nodeGroup.Name),
		})

		storagePlans := make([]*storageplan.StoragePlan, len(nodeGroup.BareMetalHosts))

		for i, host := range nodeGroup.BareMetalHosts {
			nodeCtx := logger.AppendSlogAttributesToCtx(nodeGroupCtx, []slog.Attr{
				slog.String("server-id", host.ServerID),
			})

			storagePlan := h.generateStoragePlan(nodeCtx,

				host,
				privateKey,

				hetznerConfig.BareMetal.InstallImage.VG0.RootVolumeSize,
				hetznerConfig.BareMetal.ZFS.Size,
			)
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

		// If the nodegroup's ZFS disks are NVMe, automatically add disk=nvme to the node labels.
		// This ensures NVMe-specific DaemonSets (e.g. zfs-localpv-node) are only scheduled on
		// nodes that actually have NVMe storage, preventing misscheduling on control-plane nodes.
		if len(storagePlans[0].ZFS) > 0 && storagePlans[0].ZFS[0].Type == constants.DiskTypeNVMe {
			if nodeGroup.Labels == nil {
				nodeGroup.Labels = make(map[string]string)
			}
			nodeGroup.Labels["disk"] = "nvme"
		}
	}

	allStoragePlans.GetApproval(ctx)
}

// Generates and returns storage-plan for the given Hetzner Bare Metal server.
func (h *Hetzner) generateStoragePlan(ctx context.Context,

	host *config.HetznerBareMetalHost,
	privateKey string,

	osSize,
	zfsPoolSize int,
) *storageplan.StoragePlan {
	// Fetch the server's public IPv4 address.
	address := h.getHetznerBareMetalServerIP(ctx, host.ServerID)

	// Open an SSH connection to the server.
	connection, err := ssh.NewConnection(ssh.NewConnector(ctx), ssh.Opts{
		Context: ctx,

		Hostname:   address,
		Port:       22,
		Username:   "root",
		PrivateKey: []byte(privateKey),

		Timeout: time.Second * 10,
	})
	assert.AssertErrNil(ctx, err, "Failed opening SSH connection")
	defer connection.Close()

	commandExecutor := commandexecutor.NewSSHCommandExecutor(connection)

	storagePlan, err := storageplanner.GenerateStoragePlan(ctx,

		host.ServerID,
		commandExecutor,

		osSize,
		zfsPoolSize,
	)
	assert.AssertErrNil(ctx, err, "Failed generating storage plan")

	return storagePlan
}
