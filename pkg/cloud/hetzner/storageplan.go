// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner/storageplan"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (h *Hetzner) GenerateStoragePlans(ctx context.Context, hetznerConfig *config.HetznerConfig) error {
	allStoragePlans := make(storageplan.StoragePlans)

	privateKey := hetznerConfig.SSHKeyPair.PrivateKey

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

			sp, err := h.generateStoragePlan(nodeCtx,
				host,
				privateKey,
				hetznerConfig.BareMetal.InstallImage.VG0.RootVolumeSize,
				hetznerConfig.BareMetal.ZFS.Size,
			)
			if err != nil {
				return fmt.Errorf("control-plane server %s: %w", host.ServerID, err)
			}
			storagePlans[i] = sp

			host.WWNs = []string{}
			for _, disk := range sp.OS {
				host.WWNs = append(host.WWNs, disk.WWN)
			}
		}

		if !storageplan.AreStoragePlansAlike(storagePlans) {
			return fmt.Errorf("control-plane storage plans aren't alike")
		}

		allStoragePlans["control-plane"] = storagePlans
		hetznerConfig.ControlPlane.BareMetal.StoragePlan = *storagePlans[0]
	}

	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		nodeGroupCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("node-group", nodeGroup.Name),
		})

		storagePlans := make([]*storageplan.StoragePlan, len(nodeGroup.BareMetalHosts))

		for i, host := range nodeGroup.BareMetalHosts {
			nodeCtx := logger.AppendSlogAttributesToCtx(nodeGroupCtx, []slog.Attr{
				slog.String("server-id", host.ServerID),
			})

			sp, err := h.generateStoragePlan(nodeCtx,
				host,
				privateKey,
				hetznerConfig.BareMetal.InstallImage.VG0.RootVolumeSize,
				hetznerConfig.BareMetal.ZFS.Size,
			)
			if err != nil {
				return fmt.Errorf("node-group %s server %s: %w", nodeGroup.Name, host.ServerID, err)
			}
			storagePlans[i] = sp

			host.WWNs = []string{}
			for _, disk := range sp.OS {
				host.WWNs = append(host.WWNs, disk.WWN)
			}
		}

		if !storageplan.AreStoragePlansAlike(storagePlans) {
			return fmt.Errorf("node-group %s storage plans aren't alike", nodeGroup.Name)
		}

		allStoragePlans[nodeGroup.Name] = storagePlans
		nodeGroup.StoragePlan = *storagePlans[0]
	}

	allStoragePlans.GetApproval(ctx)
	return nil
}

func (h *Hetzner) generateStoragePlan(ctx context.Context,
	host *config.HetznerBareMetalHost,
	privateKey string,
	osSize,
	zfsPoolSize int,
) (*storageplan.StoragePlan, error) {
	address, err := h.getHetznerBareMetalServerIP(host.ServerID)
	if err != nil {
		return nil, fmt.Errorf("getting server IP: %w", err)
	}

	// Reuse the SSH connection isHBMSReachable opened during the
	// install-wait phase. Cache-hit ⇒ no new TCP+KEX, no new yubikey
	// touch. If the cache somehow missed (operator skipped the OS
	// install via idempotency, host SSH-reachable from the start),
	// the pool opens fresh and tells the operator with a touch hint;
	// the same connection then services every storage-plan command
	// (lsblk + ethtool + …) over its single authenticated channel.
	connection, err := h.sshPool.getOrOpen(ctx, address, privateKey,
		fmt.Sprintf("scan disks on Hetzner bare-metal server at %s", address),
	)
	if err != nil {
		return nil, fmt.Errorf("opening SSH connection to %s: %w", address, err)
	}

	commandExecutor := commandexecutor.NewSSHCommandExecutor(connection)

	sp, err := storageplanner.GenerateStoragePlan(ctx,
		host.ServerID,
		commandExecutor,
		osSize,
		zfsPoolSize,
	)
	if err != nil {
		return nil, fmt.Errorf("generating storage plan: %w", err)
	}

	return sp, nil
}
