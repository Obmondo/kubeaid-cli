// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// Config is the explicit, per-cluster input of the library packages
// (pkg/cloud/hetzner, pkg/utils/kubernetes, pkg/utils/git, pkg/templates). They read it instead
// of ParsedGeneralConfig, ParsedSecretsConfig and the globals package, so one process can handle
// any number of clusters at once.
//
// Build it with New. General and Secrets are used in place, never copied: library code hydrates
// them as it goes (for example the Hetzner SSH key name), and callers read the result back from
// the same structs. A Config is not safe for concurrent use.
type Config struct {
	General *GeneralConfig
	Secrets *SecretsConfig

	// CloudProviderName is one of constants.CloudProvider*. New derives it from General.
	CloudProviderName string

	// CAPIClusterNamespace is the management-cluster namespace holding the cluster's Cluster API
	// resources and its cloud-credentials Secret. Empty means
	// constants.DefaultCAPIClusterNamespace; read it through CAPINamespace.
	CAPIClusterNamespace string

	// Provisioned is written by the provisioning code and read when rendering.
	Provisioned ProvisionedInfrastructure
}

// ProvisionedInfrastructure records what Hetzner prerequisite provisioning created or found,
// for later phases (template rendering, DNS gates) to read. Zero values mean "not provisioned".
type ProvisionedInfrastructure struct {
	// ControlPlaneLBPrivateIP is the pre-created control-plane load balancer's private IP.
	ControlPlaneLBPrivateIP string

	// ControlPlaneHostname is the configured control-plane endpoint hostname; empty when the
	// load balancer's private IP is the endpoint.
	ControlPlaneHostname string

	// ControlPlaneLBBootstrapPublicIP is the load balancer's public IPv4, which the hostname
	// resolves to during bootstrap. Set only together with ControlPlaneHostname.
	ControlPlaneLBBootstrapPublicIP string

	// CoturnFloatingIPs are the Floating IPs allocated for NetBird Coturn on a multi-CP HCloud
	// VPN cluster.
	CoturnFloatingIPs []string
}

// New returns the Config for general and secrets, deriving CloudProviderName from general.
func New(general *GeneralConfig, secrets *SecretsConfig) (*Config, error) {
	if general == nil {
		return nil, errors.New("general config is nil")
	}
	if secrets == nil {
		return nil, errors.New("secrets config is nil")
	}

	providerName := general.Cloud.ProviderName()
	if providerName == "" {
		return nil, errors.New("no cloud-provider specific details provided")
	}

	return &Config{
		General:           general,
		Secrets:           secrets,
		CloudProviderName: providerName,
	}, nil
}

// CAPINamespace returns the management-cluster namespace of the cluster's Cluster API resources.
func (c *Config) CAPINamespace() string {
	if c == nil || c.CAPIClusterNamespace == "" {
		return constants.DefaultCAPIClusterNamespace
	}
	return c.CAPIClusterNamespace
}

// ProviderName returns the constants.CloudProvider* value for the cloud block that is set, or ""
// when none is.
func (c *CloudConfig) ProviderName() string {
	switch {
	case c.AWS != nil:
		return constants.CloudProviderAWS

	case c.Azure != nil:
		return constants.CloudProviderAzure

	case c.Hetzner != nil:
		return constants.CloudProviderHetzner

	case c.BareMetal != nil:
		return constants.CloudProviderBareMetal

	case c.Local != nil:
		return constants.CloudProviderLocal

	default:
		return ""
	}
}
