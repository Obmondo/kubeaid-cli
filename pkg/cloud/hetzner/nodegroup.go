// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// For each Hetzner Bare Metal node group, if the ZFS disks are NVMe,
// automatically add disk=nvme to the node labels.
// This ensures NVMe-specific DaemonSets (e.g. zfs-localpv-node) are only scheduled on
// nodes that actually have NVMe storage, preventing misscheduling on control-plane nodes.
//
// Must be called after GenerateStoragePlans has populated nodeGroup.StoragePlan.
func hydrateNodeGroupLabels(hetznerConfig *config.HetznerConfig) {
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		if len(nodeGroup.StoragePlan.ZFS) > 0 && nodeGroup.StoragePlan.ZFS[0].Type == constants.DiskTypeNVMe {
			if nodeGroup.Labels == nil {
				nodeGroup.Labels = make(map[string]string)
			}
			nodeGroup.Labels["disk"] = "nvme"
		}
	}
}
