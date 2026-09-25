// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

var (
	// securityOperationsTenantCodePattern is the umbrella chart's rule for a
	// tenant code (values.schema.json): it becomes a namespace suffix, a
	// Keycloak group suffix, a host name label and a search alias.
	securityOperationsTenantCodePattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

	securityOperationsDigitsPattern = regexp.MustCompile(`^[0-9]+$`)

	securityOperationsHostnamePattern = regexp.MustCompile(
		`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`,
	)

	securityOperationsHostPrefixPattern = regexp.MustCompile(`^[a-z0-9.-]*$`)
)

const maxPort = 65535

// hydrateSecurityOperationsDefaults fills the derived defaults of
// cluster.securityOperations: realm, Keycloak URL (from cluster.keycloak.dns),
// agent port base, ingress class, cluster issuer, reconciler dry run, indexer
// replicas, and the agent ports of tenants with an all-digit code. No-op when
// the block is absent. Validation is validateSecurityOperationsConfig's job.
func hydrateSecurityOperationsDefaults() {
	cluster := &config.ParsedGeneralConfig.Cluster
	cfg := cluster.SecurityOperations
	if cfg == nil {
		return
	}

	if cfg.Keycloak.Realm == "" {
		cfg.Keycloak.Realm = constants.SecurityOperationsDefaultRealm
	}
	if cfg.Keycloak.URL == "" && cluster.Keycloak != nil && cluster.Keycloak.DNS != "" {
		cfg.Keycloak.URL = "https://" + cluster.Keycloak.DNS + "/auth"
	}
	if cfg.AgentPortBase == 0 {
		cfg.AgentPortBase = constants.SecurityOperationsDefaultAgentPortBase
	}
	if cfg.IngressClassName == "" {
		cfg.IngressClassName = constants.SecurityOperationsDefaultIngressClass
	}
	if cfg.ClusterIssuer == "" {
		cfg.ClusterIssuer = constants.ClusterIssuerLetsEncrypt
	}
	if cfg.Reconciler.DryRun == nil {
		dryRun := true
		cfg.Reconciler.DryRun = &dryRun
	}

	for i := range cfg.Tenants {
		tenant := &cfg.Tenants[i]
		if tenant.IndexerReplicas == 0 {
			tenant.IndexerReplicas = 1
		}
		if tenant.AgentPorts == nil {
			if ports, ok := derivedAgentPorts(cfg.AgentPortBase, tenant.Code); ok {
				tenant.AgentPorts = ports
			}
		}
	}
}

// derivedAgentPorts returns base + code*10 + 5 (registration) and
// base + code*10 + 4 (events) for an all-digit code; false otherwise, or when
// the code is too large to be a number.
func derivedAgentPorts(base int, code string) (*config.SecurityOperationsAgentPorts, bool) {
	if !securityOperationsDigitsPattern.MatchString(code) {
		return nil, false
	}
	n, err := strconv.Atoi(code)
	if err != nil || n > maxPort {
		return nil, false
	}
	return &config.SecurityOperationsAgentPorts{
		Registration: base + n*10 + 5,
		Events:       base + n*10 + 4,
	}, true
}

// validateSecurityOperationsConfig checks cluster.securityOperations once
// defaults are in: required fields, tenant codes and names, and that every
// agent port is valid and used once. A disabled block is not checked.
func validateSecurityOperationsConfig() error {
	cfg := config.ParsedGeneralConfig.Cluster.SecurityOperations
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	if !securityOperationsHostnamePattern.MatchString(cfg.Domain) {
		return fmt.Errorf("domain must be a fully qualified domain name (got %q)", cfg.Domain)
	}
	if !securityOperationsHostPrefixPattern.MatchString(cfg.HostPrefix) {
		return fmt.Errorf("hostPrefix may only hold lower-case letters, digits, dots and dashes (got %q)",
			cfg.HostPrefix)
	}

	if cfg.Keycloak.URL == "" {
		return errors.New("keycloak.url is required when cluster.keycloak is not set")
	}
	if u, err := url.Parse(cfg.Keycloak.URL); err != nil ||
		(u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("keycloak.url must be an http(s) URL (got %q)", cfg.Keycloak.URL)
	}

	if cfg.Keycloak.HostAliasIP != "" && net.ParseIP(cfg.Keycloak.HostAliasIP) == nil {
		return fmt.Errorf("keycloak.hostAliasIP must be an IP address (got %q)", cfg.Keycloak.HostAliasIP)
	}

	if !securityOperationsHostnamePattern.MatchString(cfg.AgentHost) {
		return fmt.Errorf("agentHost must be a fully qualified domain name (got %q)", cfg.AgentHost)
	}
	if net.ParseIP(cfg.AgentAddress) == nil {
		return fmt.Errorf("agentAddress must be an IP address (got %q)", cfg.AgentAddress)
	}
	if cfg.AgentPortBase < 1 || cfg.AgentPortBase > maxPort {
		return fmt.Errorf("agentPortBase must be between 1 and %d (got %d)", maxPort, cfg.AgentPortBase)
	}

	return validateSecurityOperationsTenants(cfg.Tenants)
}

func validateSecurityOperationsTenants(tenants []config.SecurityOperationsTenant) error {
	codes := map[string]bool{}
	names := map[string]bool{}
	ports := map[int]string{}

	usePort := func(port int, owner string) error {
		if port < 1 || port > maxPort {
			return fmt.Errorf("%s: port %d is outside 1-%d", owner, port, maxPort)
		}
		if other, taken := ports[port]; taken {
			return fmt.Errorf("%s: port %d is already used by %s", owner, port, other)
		}
		ports[port] = owner
		return nil
	}

	for i, tenant := range tenants {
		if !securityOperationsTenantCodePattern.MatchString(tenant.Code) {
			return fmt.Errorf("tenants[%d].code must match %s (got %q)",
				i, securityOperationsTenantCodePattern, tenant.Code)
		}
		if codes[tenant.Code] {
			return fmt.Errorf("tenants[%d].code %q is used twice", i, tenant.Code)
		}
		codes[tenant.Code] = true

		if tenant.Name == "" {
			return fmt.Errorf("tenants[%d] (%s): name is required", i, tenant.Code)
		}
		// The Applications land in the argocd-apps Helm chart, which would
		// read template delimiters in a name as a template.
		if strings.Contains(tenant.Name, "{{") || strings.Contains(tenant.Name, "}}") ||
			strings.ContainsAny(tenant.Name, "\n\r\t") {
			return fmt.Errorf("tenants[%d] (%s): name must not contain {{, }} or control characters",
				i, tenant.Code)
		}
		if names[tenant.Name] {
			return fmt.Errorf("tenants[%d] (%s): name %q is used twice", i, tenant.Code, tenant.Name)
		}
		names[tenant.Name] = true

		if tenant.RetentionDays < 0 {
			return fmt.Errorf("tenants[%d] (%s): retentionDays must not be negative", i, tenant.Code)
		}
		if tenant.IndexerReplicas < 1 {
			return fmt.Errorf("tenants[%d] (%s): indexerReplicas must be at least 1", i, tenant.Code)
		}

		if tenant.AgentPorts == nil {
			return fmt.Errorf(
				"tenants[%d] (%s): agentPorts is required - ports are only derived from agentPortBase for an all-digit code",
				i, tenant.Code)
		}
		owner := fmt.Sprintf("tenant %s", tenant.Code)
		if err := usePort(tenant.AgentPorts.Registration, owner+" registration"); err != nil {
			return err
		}
		if err := usePort(tenant.AgentPorts.Events, owner+" events"); err != nil {
			return err
		}
	}

	return nil
}
