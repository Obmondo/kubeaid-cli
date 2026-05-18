// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"fmt"

	"github.com/Nerzal/gocloak/v13"
)

// ProtocolMapperSpec describes a single protocol mapper attached
// to a client scope. Config carries the mapper-type-specific keys
// (e.g. "included.client.audience" for an audience mapper).
type ProtocolMapperSpec struct {
	Name           string
	Protocol       string // "openid-connect"
	ProtocolMapper string // e.g. "oidc-audience-mapper"
	Config         map[string]string
}

// ReconcileProtocolMapperOnClientScope ensures a protocol mapper
// with spec.Name exists on the client scope identified by name and
// that its config matches spec.Config. Idempotent on Name + Config —
// missing mapper is created, drifted mapper is updated, otherwise
// no-op. Only the config keys the spec explicitly sets are diffed,
// so unrelated keys (operator customizations, Keycloak defaults)
// aren't overwritten.
func (r *Reconciler) ReconcileProtocolMapperOnClientScope(
	ctx context.Context,
	realm, clientScopeName string,
	spec ProtocolMapperSpec,
) error {
	scopes, err := r.api.GetClientScopes(ctx, r.token, realm)
	if err != nil {
		return fmt.Errorf("listing client scopes in realm %q: %w", realm, err)
	}
	scope := findClientScopeByName(scopes, clientScopeName)
	if scope == nil || scope.ID == nil {
		return fmt.Errorf("client scope %q not found in realm %q", clientScopeName, realm)
	}

	desired := buildProtocolMapper(spec)

	if scope.ProtocolMappers != nil {
		for _, m := range *scope.ProtocolMappers {
			if m.Name == nil || *m.Name != spec.Name {
				continue
			}
			if protocolMapperConfigMatches(m.ProtocolMappersConfig, spec.Config) {
				return nil
			}
			// Preserve the server-side ID — Keycloak's PUT routes
			// on it; a missing/changed ID would be interpreted as
			// "create another mapper of the same name", which the
			// admin API rejects.
			desired.ID = m.ID
			if err := r.api.UpdateClientScopeProtocolMapper(ctx, r.token, realm, *scope.ID, desired); err != nil {
				return fmt.Errorf(
					"updating protocol mapper %q on client scope %q in realm %q: %w",
					spec.Name, clientScopeName, realm, err,
				)
			}
			return nil
		}
	}

	if _, err := r.api.CreateClientScopeProtocolMapper(ctx, r.token, realm, *scope.ID, desired); err != nil {
		return fmt.Errorf(
			"creating protocol mapper %q on client scope %q in realm %q: %w",
			spec.Name, clientScopeName, realm, err,
		)
	}
	return nil
}

// buildProtocolMapper translates ProtocolMapperSpec into the
// gocloak representation. Pulled out so create and update paths
// emit the same body — and so a spec that adds a new config key
// only needs to be threaded through here and the matcher below.
func buildProtocolMapper(spec ProtocolMapperSpec) gocloak.ProtocolMappers {
	return gocloak.ProtocolMappers{
		Name:           gocloak.StringP(spec.Name),
		Protocol:       gocloak.StringP(spec.Protocol),
		ProtocolMapper: gocloak.StringP(spec.ProtocolMapper),
		ProtocolMappersConfig: &gocloak.ProtocolMappersConfig{
			IncludedClientAudience: configValue(spec.Config, "included.client.audience"),
			IDTokenClaim:           configValue(spec.Config, "id.token.claim"),
			AccessTokenClaim:       configValue(spec.Config, "access.token.claim"),
			UserinfoTokenClaim:     configValue(spec.Config, "userinfo.token.claim"),
			UserAttribute:          configValue(spec.Config, "user.attribute"),
			ClaimName:              configValue(spec.Config, "claim.name"),
			ClaimValue:             configValue(spec.Config, "claim.value"),
			JSONTypeLabel:          configValue(spec.Config, "jsonType.label"),
			Multivalued:            configValue(spec.Config, "multivalued"),
			FullPath:               configValue(spec.Config, "full.path"),
		},
	}
}

// protocolMapperConfigMatches reports whether existing's stored
// values agree with every key the caller explicitly set in desired.
// Keys the caller didn't set are ignored — they may be operator
// customizations or Keycloak defaults we don't manage.
//
// The pair list is the projection from raw config keys onto the
// gocloak typed view; it must stay in lockstep with the field set
// buildProtocolMapper populates.
func protocolMapperConfigMatches(existing *gocloak.ProtocolMappersConfig, desired map[string]string) bool {
	if existing == nil {
		return len(desired) == 0
	}
	pairs := []struct {
		key string
		ptr *string
	}{
		{"included.client.audience", existing.IncludedClientAudience},
		{"id.token.claim", existing.IDTokenClaim},
		{"access.token.claim", existing.AccessTokenClaim},
		{"userinfo.token.claim", existing.UserinfoTokenClaim},
		{"user.attribute", existing.UserAttribute},
		{"claim.name", existing.ClaimName},
		{"claim.value", existing.ClaimValue},
		{"jsonType.label", existing.JSONTypeLabel},
		{"multivalued", existing.Multivalued},
		{"full.path", existing.FullPath},
	}
	for _, p := range pairs {
		want, set := desired[p.key]
		if !set {
			continue
		}
		got := ""
		if p.ptr != nil {
			got = *p.ptr
		}
		if got != want {
			return false
		}
	}
	return true
}

func configValue(cfg map[string]string, key string) *string {
	if v, ok := cfg[key]; ok {
		return gocloak.StringP(v)
	}
	return nil
}
