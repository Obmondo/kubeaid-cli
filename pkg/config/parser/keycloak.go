// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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
//   - cluster.type=vpn => keycloak block is mandatory (the VPN
//     cluster ALWAYS runs on top of Keycloak, managed or external)
//   - cluster.type=workload + keycloak block => mode must be
//     external. Workload clusters never host Keycloak; they only
//     reference one (typically a parent VPN cluster's) for OIDC
//     derivation.
//   - cluster.type=workload + no keycloak block => allowed; the
//     cluster boots without OIDC and the operator authenticates with
//     admin.conf (the workload bootstrap also prints a warning).
//   - mode in {managed, external}; VPN-only invariants (NetBird,
//     ACME, external-backend secret) only apply when type=vpn.
//
// The mode only switches whether kubeaid-cli installs the keycloakx
// chart, runs the gocloak realm reconciler, and writes the
// keycloak-admin SealedSecret. Everything else (cnpg for Postgres,
// traefik, cert-manager LE issuer, netbird Secret, post-sync DSN
// patch) is needed by both modes — but only on VPN clusters.
func validateKeycloakConfig() error {
	cluster := &config.ParsedGeneralConfig.Cluster
	cfg := cluster.Keycloak

	if cluster.Type == constants.ClusterTypeVPN && cfg == nil {
		return errors.New(
			"cluster.keycloak is required when cluster.type=vpn — VPN clusters always run on top of a Keycloak (managed by kubeaid-cli or external)",
		)
	}

	if cfg == nil {
		return nil
	}

	// Workload clusters access via the NetBird mesh, not Keycloak/OIDC —
	// a keycloak block here is inert and misleading.
	if cluster.Type != constants.ClusterTypeVPN {
		return errors.New(
			"cluster.keycloak is not supported on workload clusters — access is via the NetBird mesh (set cluster.netbird.dns)",
		)
	}

	if cfg.Mode != constants.KeycloakModeManaged && cfg.Mode != constants.KeycloakModeExternal {
		return fmt.Errorf(
			"cluster.keycloak.mode must be %q or %q (got %q)",
			constants.KeycloakModeManaged, constants.KeycloakModeExternal, cfg.Mode,
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

	// Both modes need the netbird block (every VPN cluster runs the
	// NetBird mesh; OIDC client redirect URIs and Mgmt ingress
	// hostname are derived from this).
	if cluster.NetBird == nil || cluster.NetBird.DNS == "" {
		return errors.New(
			"cluster.netbird.dns is required for VPN clusters — used for OIDC client redirect URIs and Mgmt ingress hostname",
		)
	}

	// ACME email is needed for the LE ClusterIssuer that mints TLS
	// certs for the NetBird Mgmt Ingress (in external mode the
	// Keycloak Ingress is the operator's problem; the NetBird side
	// still flows through our traefik+LE).
	if cluster.ACMEEmail == "" {
		return errors.New(
			"cluster.acmeEmail is required for VPN clusters — used to register the Let's Encrypt account that issues TLS certs for the NetBird Ingress",
		)
	}

	// External mode: the netbird-backend OIDC client lives in the
	// operator's external Keycloak; only they know its client
	// secret. The validator runs after secrets.yaml is parsed, so
	// surface the missing field clearly here rather than letting
	// kubeaid-cli emit a netbird Secret with an empty client
	// secret and fail at runtime.
	if cfg.Mode == constants.KeycloakModeExternal {
		creds := config.ParsedSecretsConfig.Keycloak
		if creds == nil || creds.NetBirdBackendClientSecret == "" {
			return errors.New(
				"secrets.yaml: keycloak.netBirdBackendClientSecret is required when cluster.keycloak.mode=external — kubeaid-cli can't generate it because the netbird-backend client lives in the operator's external Keycloak",
			)
		}
	}

	return nil
}
