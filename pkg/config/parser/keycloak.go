// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// hydrateKeycloakDefaults applies the derived defaults for the
// cluster.keycloak block — currently just defaulting the realm name
// from the DNS when the user hasn't set it explicitly. It does NOT
// validate; that's validateKeycloakConfig's job. No-op when the block
// is absent.
//
// Realm derivation:
//
//	keycloak.vpn.acme.com  → publicsuffix → "acme.com" → "acme"
//	keycloak.foo.co.uk     → publicsuffix → "foo.co.uk" → "foo"
//
// publicsuffix handles multi-part TLDs (`.co.uk`, `.com.au`) correctly,
// avoiding the naive "split on dot, drop last" bug.
func hydrateKeycloakDefaults() {
	cfg := config.ParsedGeneralConfig.Cluster.Keycloak
	if cfg == nil {
		return
	}

	if cfg.Realm == "" && cfg.DNS != "" {
		cfg.Realm = deriveRealm(cfg.DNS)
	}
}

// deriveRealm returns the first dot-separated segment of the
// effective TLD-plus-one for host. Empty string if host is empty,
// has no public suffix, or is otherwise unworkable — the caller's
// validation will then surface a clear error.
func deriveRealm(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	etldPlusOne, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}

	// "acme.com" → "acme"; "foo.co.uk" → "foo"
	return strings.SplitN(etldPlusOne, ".", 2)[0]
}

// validateKeycloakConfig enforces the cross-field rules that
// struct-tag validation can't express:
//
//   - cluster.type=vpn => keycloak block is mandatory
//   - keycloak.mode=managed => only valid with cluster.type=vpn
//     (a workload cluster cannot host its own Keycloak; it inherits
//     OIDC from a parent VPN cluster)
//   - after default-derivation, realm must be non-empty
func validateKeycloakConfig() error {
	cluster := &config.ParsedGeneralConfig.Cluster

	if cluster.Type == constants.ClusterTypeVPN && cluster.Keycloak == nil {
		return errors.New(
			"cluster.keycloak is required when cluster.type=vpn — VPN clusters always provision a managed Keycloak",
		)
	}

	cfg := cluster.Keycloak
	if cfg == nil {
		return nil
	}

	if cfg.Mode == "managed" && cluster.Type != constants.ClusterTypeVPN {
		return fmt.Errorf(
			"cluster.keycloak.mode=managed is only valid when cluster.type=vpn (got %q) — workload clusters inherit OIDC from a parent VPN cluster",
			cluster.Type,
		)
	}

	if cfg.DNS == "" {
		return errors.New("cluster.keycloak.dns is required")
	}

	if cfg.Realm == "" {
		return fmt.Errorf(
			"cluster.keycloak.realm could not be derived from DNS %q — set it explicitly",
			cfg.DNS,
		)
	}

	// Managed Keycloak provisions NetBird's OIDC clients in the
	// same realm; the netbird block is required so kubeaid-cli
	// knows the public NetBird Mgmt URL for redirect URIs and the
	// audience claim.
	if cfg.Mode == "managed" {
		if cluster.NetBird == nil || cluster.NetBird.DNS == "" {
			return errors.New(
				"cluster.netbird.dns is required when cluster.keycloak.mode=managed — kubeaid-cli renders NetBird's OIDC client against this hostname",
			)
		}

		// Managed Keycloak's Ingress is served by traefik with TLS
		// certs minted by cert-manager via a Let's Encrypt
		// ClusterIssuer. The ACME account registration needs an
		// email address so LE can send expiry warnings; without it
		// the ClusterIssuer never reaches Ready and no certs get
		// issued.
		if cluster.ACMEEmail == "" {
			return errors.New(
				"cluster.acmeEmail is required when cluster.keycloak.mode=managed — used to register the Let's Encrypt account that issues TLS certs for keycloak/netbird Ingresses",
			)
		}
	}

	return nil
}
