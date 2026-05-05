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
// with spec.Name exists on the client scope identified by name.
// Idempotent on Name within the scope.
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

	if scope.ProtocolMappers != nil {
		for _, m := range *scope.ProtocolMappers {
			if m.Name != nil && *m.Name == spec.Name {
				return nil
			}
		}
	}

	cfg := make(map[string]string, len(spec.Config))
	for k, v := range spec.Config {
		cfg[k] = v
	}

	mapper := gocloak.ProtocolMappers{
		Name:           gocloak.StringP(spec.Name),
		Protocol:       gocloak.StringP(spec.Protocol),
		ProtocolMapper: gocloak.StringP(spec.ProtocolMapper),
		ProtocolMappersConfig: &gocloak.ProtocolMappersConfig{
			IncludedClientAudience: configValue(cfg, "included.client.audience"),
			IDTokenClaim:           configValue(cfg, "id.token.claim"),
			AccessTokenClaim:       configValue(cfg, "access.token.claim"),
			UserinfoTokenClaim:     configValue(cfg, "userinfo.token.claim"),
			UserAttribute:          configValue(cfg, "user.attribute"),
			ClaimName:              configValue(cfg, "claim.name"),
			ClaimValue:             configValue(cfg, "claim.value"),
			JSONTypeLabel:          configValue(cfg, "jsonType.label"),
			Multivalued:            configValue(cfg, "multivalued"),
		},
	}

	if _, err := r.api.CreateClientScopeProtocolMapper(ctx, r.token, realm, *scope.ID, mapper); err != nil {
		return fmt.Errorf(
			"creating protocol mapper %q on client scope %q in realm %q: %w",
			spec.Name, clientScopeName, realm, err,
		)
	}
	return nil
}

func configValue(cfg map[string]string, key string) *string {
	if v, ok := cfg[key]; ok {
		return gocloak.StringP(v)
	}
	return nil
}
