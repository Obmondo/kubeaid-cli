// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package render turns a cluster's answers into general.yaml and
// secrets.yaml by executing the same templates kubeaid-cli's own
// interactive flow uses.
//
// Deliberately a leaf: stdlib, plus pkg/constants and pkg/urlprotocol which
// are leaves themselves. That is what lets the Obmondo API import this to
// render server-side instead of vendoring a copy of the templates and
// testing it for drift. Nothing here touches the filesystem, the network or
// a terminal — keep it that way, or the import stops being cheap.
package render

import (
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/urlprotocol"
)

// ObmondoConfig is the obmondo block of general.yaml.
//
// Defined here rather than in pkg/config because pkg/config reaches ~1180
// packages, and PromptedConfig embeds this type: leaving it there would
// drag all of that into every importer of this package. pkg/config aliases
// it, so its own callers and the yaml tags are unchanged.
type ObmondoConfig struct {
	CustomerID string `yaml:"customerID"`
	Monitoring bool   `yaml:"monitoring"`

	// Path to the mTLS client cert issued by Obmondo. Required when
	// Monitoring is true — kubeaid-agent uses it to authenticate to the
	// Obmondo API, and kube-prometheus's Alertmanager uses it to push
	// alerts to Obmondo's alert-receiver endpoint.
	CertPath string `yaml:"certPath"`

	// Path to the private key paired with CertPath. Required when
	// Monitoring is true.
	KeyPath string `yaml:"keyPath"`
}

type PromptedConfig struct {
	// ConfigsDirectory is the on-disk path the rendered general.yaml
	// and secrets.yaml are written to. Not rendered into the
	// templates — held only so the Hetzner bare-metal add-loop can
	// scan sibling cluster directories for already-used server IDs.
	ConfigsDirectory string

	// Cluster.
	ClusterName           string
	ClusterType           string
	K8sVersion            string
	KubePrometheusVersion string
	EnableAuditLogging    bool

	// Keycloak reference fields, VPN clusters only (kubeaid-cli installs
	// or references Keycloak as NetBird's IdP). Render the
	// cluster.keycloak.{mode,dns,realm} block in general.yaml.
	KeycloakMode  string // "managed" | "external"
	KeycloakDNS   string
	KeycloakRealm string

	// NetBirdDNS is the NetBird Management endpoint. Set for VPN clusters
	// (the host) and for workload clusters that join a mesh. Rendered to
	// cluster.netbird.dns.
	NetBirdDNS string
	ACMEEmail  string // VPN-only

	// NetBirdDNSZone is the mesh DNS domain (NetBird --dns-domain). Required
	// for vpn clusters and for workload clusters that join a mesh; empty for
	// workload clusters that declined (no cluster.netbird block rendered).
	// Written to cluster.netbird.dnsZone.
	NetBirdDNSZone string

	// NetBirdBackendClientSecret is collected only when KeycloakMode
	// is "external" — kubeaid-cli has no way to mint or look up the
	// netbird-backend client secret in the operator's external
	// Keycloak. Rendered into secrets.yaml under
	// keycloak.netBirdBackendClientSecret. Empty when managed.
	NetBirdBackendClientSecret string

	// NetBirdAPIKey is the NetBird service-user PAT the netbird-operator
	// authenticates with, collected when a workload cluster joins a mesh.
	// Rendered into secrets.yaml netbird.apiKey.
	NetBirdAPIKey string

	// Lockdown is the workload Host Firewall (CCNP) decision: nil when not
	// asked, else the operator's choice. Rendered to cluster.lockdown.
	Lockdown *bool

	// HCloud-VPN control-plane endpoint FQDN — required when
	// running a VPN cluster on Hetzner HCloud. Rendered into
	// cloud.hetzner.controlPlane.hcloud.loadBalancer.endpoint;
	// must resolve (post-DNS-setup) to the LB's public IP during
	// bootstrap and to its private IP afterwards.
	ControlPlaneEndpoint string

	// Git.
	UseSSHAgent bool
	SSHKeyPath  string
	SSHUsername string

	// Forks.
	KubeaidForkURL       string
	KubeaidVersion       string
	KubeaidConfigForkURL string
	KubeaidConfigDir     string

	// ArgoCD deploy keys.
	KubeaidConfigDeployKeyPath string

	// GitKnownHosts holds known_hosts lines captured at prompt time
	// for SSH-form fork URLs whose host isn't already in the
	// embedded common-providers list (github / gitlab / azure /
	// bitbucket). Persisted into git.knownHosts in general.yaml so
	// subsequent kubeaid-cli runs work offline.
	GitKnownHosts []string

	// Cloud provider.
	CloudProvider string

	// AWS.
	AWSRegion string
	// AWSEKS selects an AWS managed (EKS) control plane instead of the
	// self-managed kubeadm one. Skips the HA / AMI / SSH-key questions.
	AWSEKS bool
	// AWSNodeInstanceType sizes the default worker node-group; independent of
	// the control-plane type so workers can be beefier than the CP.
	AWSNodeInstanceType string
	AWSSSHKeyName       string
	AWSCPInstanceType   string
	AWSCPReplicas       string
	AWSAMIID            string
	AWSAccessKeyID      string
	AWSSecretAccessKey  string
	AWSSessionToken     string

	// Azure.
	AzureTenantID       string
	AzureSubscriptionID string
	AzureLocation       string
	AzureStorageAccount string
	AzureCPVMSize       string
	AzureCPReplicas     string
	AzureCPDiskSizeGB   string
	AzureClientID       string
	AzureClientSecret   string

	// Hetzner.
	HetznerMode          string
	HetznerSSHKeyName    string
	HetznerSSHKeyPath    string
	HetznerHCloudZone    string
	HetznerCPMachineType string
	HetznerCPReplicas    string
	HetznerLBRegion      string
	HetznerRegion        string
	HetznerAPIToken      string
	HetznerRobotUser     string
	HetznerRobotPassword string

	// HetznerBMKnownServerIDs is the cached Robot inventory fetched
	// at credential-validation time (on Enter past the password
	// field). Used to seed huh.Input.Suggestions for server-ID
	// autocomplete in the BM add-loop. Transient — not rendered into
	// general.yaml or secrets.yaml.
	HetznerBMKnownServerIDs []string

	// Hetzner bare-metal — populated only when HetznerMode is
	// "bare-metal" (and, in the future, "hybrid" for the BM
	// node-group leg). Lengths line up: CPServerIDs[i] pairs with
	// CPPrivateIPs[i]; same for NodeGroupServerIDs/NodeGroupPrivateIPs.
	HetznerBMCPServerIDs          []string
	HetznerBMCPPrivateIPs         []string
	HetznerBMNodeGroupName        string
	HetznerBMNodeGroupServerIDs   []string
	HetznerBMNodeGroupPrivateIPs  []string
	HetznerBMEndpointHost         string
	HetznerBMEndpointIsFailoverIP bool
	// HetznerBMServerPublicIPs maps a Robot server ID to the public
	// IPv4 the Robot webservice returned for it at validation time.
	// Rendered as a `# id NNN → IP` comment alongside each host in
	// general.yaml so the operator can sanity-check the IDs map to
	// the boxes they expected. Not load-bearing — bootstrap re-reads
	// these via the Robot API at run time.
	HetznerBMServerPublicIPs map[string]string

	// HetznerBMCPRegions is the unique-set of Hetzner region IDs
	// (lower-case, e.g. "fsn1", "hel1", "ash") derived from each
	// chosen control-plane Robot server's DC field. Rendered into
	// global.HetznerConfig.ControlPlane.Regions so the upstream
	// CAPH chart's `minItems: 1` schema check passes — previously
	// bare-metal mode emitted `regions: []` on the theory that
	// kubeaid-cli would fill it from Robot at bootstrap, but the
	// schema validates BEFORE that runtime step ever runs.
	HetznerBMCPRegions []string

	// Hetzner vSwitch — required for hybrid mode (kubeaid-cli's
	// CreateVSwitch is called unconditionally for hybrid) and
	// reserved for the pure-bare-metal auto-create follow-up.
	// Hetzner's webservice rejects VLAN IDs outside 4000-4091, so
	// the prompt validates that range up front.
	HetznerVSwitchName       string
	HetznerVSwitchVLANID     string
	HetznerVSwitchSubnetCIDR string

	// HetznerBMKnownVSwitches is the cached Robot vSwitch inventory,
	// fetched the first time the vSwitch phase runs. Drives the
	// "reuse an existing vSwitch" picker and the next-free VLAN ID
	// default. Transient — not rendered into general.yaml.
	HetznerBMKnownVSwitches []RobotVSwitch

	// Bare Metal (generic, not Hetzner). Hosts are collected one at a time by
	// the add-loop in provider_baremetal.go, same flow as the Hetzner
	// bare-metal prompt.
	BareMetalSSHPort           string
	BareMetalEndpointHost      string
	BareMetalEndpointPort      string
	BareMetalControlPlaneHosts []string
	BareMetalWorkerHosts       []string
	BareMetalNodeGroupName     string

	Obmondo *ObmondoConfig
}

// KubeaidIsSSH reports whether KubeaidForkURL is an SSH-form Git URL.
// Used by general.yaml.tmpl to decide whether to render the kubeaid
// ArgoCD deploy key block — HTTPS public forks need no key, SSH
// forks (private) do.
func (c *PromptedConfig) KubeaidIsSSH() bool {
	return urlprotocol.UsingSSHBasedProtocol(c.KubeaidForkURL)
}

// LockdownSet reports whether cluster.lockdown should be rendered.
func (c PromptedConfig) LockdownSet() bool { return c.Lockdown != nil }

// LockdownValue is the value for the rendered cluster.lockdown line.
func (c PromptedConfig) LockdownValue() bool { return c.Lockdown != nil && *c.Lockdown }

// NetBirdBlockEnabled reports whether to render the cluster.netbird block.
// VPN clusters host NetBird by definition; workload clusters render it only
// when they joined a mesh — the join form then set the mesh DNS zone. Mirrors
// pkg/core/netbird's derive-from-config idiom (no separate enabled flag).
func (c PromptedConfig) NetBirdBlockEnabled() bool {
	return c.ClusterType == constants.ClusterTypeVPN || c.NetBirdDNSZone != ""
}

// RobotVSwitch is one Hetzner Robot vSwitch as the Robot API returns it.
//
// Not rendered into either template — PromptedConfig carries a list of
// these only so the bare-metal add-loop can seed its picker. It lives here
// because it is reachable from PromptedConfig, and exported because the
// prompt package populates the field.
type RobotVSwitch struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	VLANID int    `json:"vlan"`
	// Cancelled vSwitches are being torn down; CreateVSwitch refuses
	// to adopt one, so they're filtered out of the picker.
	Cancelled bool `json:"cancelled"`
}
