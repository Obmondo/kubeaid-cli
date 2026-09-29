// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Result kinds for identity providers.
const (
	KindIdentityProvider       = "identity-provider"
	KindIdentityProviderMapper = "identity-provider-mapper"
)

// hardcodedGroupMapper is Keycloak's "Hardcoded Group" IdP mapper; it
// works for every provider type despite the oidc- prefix.
const hardcodedGroupMapper = "oidc-hardcoded-group-idp-mapper"

// IdentityProviderSpec describes an identity-provider broker (e.g. a
// tenant's own Entra ID or ADFS) whose users all land in one group.
type IdentityProviderSpec struct {
	Alias       string
	DisplayName string
	// ProviderID is "oidc", "keycloak-oidc" or "saml".
	ProviderID string
	Enabled    bool
	TrustEmail bool
	// Config holds provider settings (authorizationUrl, tokenUrl,
	// clientId, issuer, ...). Only the listed keys are compared.
	Config map[string]string
	// ClientSecret is set on create and never compared (Keycloak
	// masks it on read).
	ClientSecret string
	// Group is the group every brokered user is put in (without the
	// leading slash). Empty means no mapper.
	Group string
}

// EnsureIdentityProvider reconciles an IdP broker and its group mapper.
func (r *Reconciler) EnsureIdentityProvider(ctx context.Context, realm string, spec IdentityProviderSpec) ([]Result, error) {
	base := realmPath(realm, "/identity-provider/instances")
	path := base + "/" + url.PathEscape(spec.Alias)

	var results []Result
	var cur map[string]any
	err := r.do(ctx, methodGet, path, nil, &cur)
	switch {
	case isRawNotFound(err):
		results = append(results, Result{Kind: KindIdentityProvider, Name: spec.Alias, Change: ChangeCreated})
		if r.dryRun {
			if spec.Group != "" {
				results = append(results, Result{Kind: KindIdentityProviderMapper, Name: spec.Alias + "/tenant-group", Change: ChangeCreated})
			}
			return results, nil
		}
		cfg := map[string]any{}
		for k, v := range spec.Config {
			cfg[k] = v
		}
		if spec.ClientSecret != "" {
			cfg["clientSecret"] = spec.ClientSecret
		}
		body := map[string]any{
			keyAlias:      spec.Alias,
			"displayName": spec.DisplayName,
			"providerId":  spec.ProviderID,
			keyEnabled:    spec.Enabled,
			"trustEmail":  spec.TrustEmail,
			keyConfig:     cfg,
		}
		if err := r.do(ctx, methodPost, base, body, nil); err != nil {
			return nil, fmt.Errorf("creating identity provider %q: %w", spec.Alias, err)
		}
	case err != nil:
		return nil, fmt.Errorf("reading identity provider %q: %w", spec.Alias, err)
	default:
		change, detail, err := r.updateIdentityProvider(ctx, path, cur, spec)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{Kind: KindIdentityProvider, Name: spec.Alias, Change: change, Detail: detail})
	}

	if spec.Group == "" {
		return results, nil
	}
	change, err := r.ensureIdPGroupMapper(ctx, path, spec)
	if err != nil {
		return results, err
	}
	results = append(results, Result{Kind: KindIdentityProviderMapper, Name: spec.Alias + "/tenant-group", Change: change})
	return results, nil
}

func (r *Reconciler) updateIdentityProvider(
	ctx context.Context, path string, cur map[string]any, spec IdentityProviderSpec,
) (Change, string, error) {
	var drift []string
	if spec.DisplayName != "" && asString(cur["displayName"]) != spec.DisplayName {
		cur["displayName"] = spec.DisplayName
		drift = append(drift, "displayName")
	}
	if asBool(cur[keyEnabled]) != spec.Enabled {
		cur[keyEnabled] = spec.Enabled
		drift = append(drift, "enabled")
	}
	if asBool(cur["trustEmail"]) != spec.TrustEmail {
		cur["trustEmail"] = spec.TrustEmail
		drift = append(drift, "trustEmail")
	}
	cfg := asMap(cur[keyConfig])
	for _, k := range sortedKeys(spec.Config) {
		if asString(cfg[k]) != spec.Config[k] {
			cfg[k] = spec.Config[k]
			drift = append(drift, "config."+k)
		}
	}
	cur[keyConfig] = cfg
	if len(drift) == 0 {
		return ChangeNone, "", nil
	}
	detail := strings.Join(drift, ",")
	if r.dryRun {
		return ChangeUpdated, detail, nil
	}
	if err := r.do(ctx, methodPut, path, cur, nil); err != nil {
		return ChangeNone, "", fmt.Errorf("updating identity provider %q (%s): %w", spec.Alias, detail, err)
	}
	return ChangeUpdated, detail, nil
}

func (r *Reconciler) ensureIdPGroupMapper(ctx context.Context, idpPath string, spec IdentityProviderSpec) (Change, error) {
	const name = "tenant-group"
	want := map[string]string{"group": "/" + strings.TrimPrefix(spec.Group, "/"), "syncMode": "INHERIT"}
	var list []map[string]any
	if err := r.do(ctx, methodGet, idpPath+"/mappers", nil, &list); err != nil {
		return ChangeNone, fmt.Errorf("listing mappers of identity provider %q: %w", spec.Alias, err)
	}
	for _, m := range list {
		if asString(m[keyName]) != name {
			continue
		}
		cfg := asMap(m[keyConfig])
		drift := asString(m["identityProviderMapper"]) != hardcodedGroupMapper
		for k, v := range want {
			if asString(cfg[k]) != v {
				cfg[k] = v
				drift = true
			}
		}
		if !drift {
			return ChangeNone, nil
		}
		if r.dryRun {
			return ChangeUpdated, nil
		}
		m["identityProviderMapper"] = hardcodedGroupMapper
		m[keyConfig] = cfg
		if err := r.do(ctx, methodPut, idpPath+"/mappers/"+url.PathEscape(asString(m["id"])), m, nil); err != nil {
			return ChangeNone, fmt.Errorf("updating group mapper of identity provider %q: %w", spec.Alias, err)
		}
		return ChangeUpdated, nil
	}
	if r.dryRun {
		return ChangeCreated, nil
	}
	body := map[string]any{
		keyName:                  name,
		"identityProviderAlias":  spec.Alias,
		"identityProviderMapper": hardcodedGroupMapper,
		keyConfig:                want,
	}
	if err := r.do(ctx, methodPost, idpPath+"/mappers", body, nil); err != nil {
		return ChangeNone, fmt.Errorf("creating group mapper of identity provider %q: %w", spec.Alias, err)
	}
	return ChangeCreated, nil
}
