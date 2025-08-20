// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func IsUsingHCloud() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHCloud) ||
		(ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHybrid)
}

func IsControlPlaneInHCloud() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHCloud) ||
		(ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHybrid)
}

func IsUsingHetznerBareMetal() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeBareMetal) ||
		(ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHybrid)
}

func IsControlPlaneInHetznerBareMetal() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeBareMetal)
}
