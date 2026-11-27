// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-cli/pkg/keycloak"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/httpx"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

const requiredActionTOTP = "CONFIGURE_TOTP"

// kcRun collects Keycloak results.
type kcRun struct {
	kc      *keycloak.Reconciler
	realm   string
	results []report.Result
}

func (k *kcRun) add(kind, name string, c keycloak.Change, detail string, err error) {
	r := report.Result{Component: ComponentKeycloak, Kind: kind, Name: name, Action: action(c), Detail: detail}
	if err != nil {
		r.Action, r.Detail = report.ActionError, err.Error()
	}
	k.results = append(k.results, r)
}

func (k *kcRun) addAll(results []keycloak.Result, err error, kind, name string) {
	for _, r := range results {
		k.add(r.Kind, r.Name, r.Change, r.Detail, nil)
	}
	if err != nil {
		k.add(kind, name, keycloak.ChangeNone, "", err)
	}
}

func action(c keycloak.Change) report.Action {
	switch c {
	case keycloak.ChangeCreated:
		return report.ActionCreate
	case keycloak.ChangeUpdated:
		return report.ActionUpdate
	case keycloak.ChangeNone:
		return report.ActionOK
	}
	return report.ActionOK
}

func runKeycloak(ctx context.Context, cfg *config.Config, store secrets.Store, dryRun bool) []report.Result {
	kcfg := cfg.Keycloak
	password, err := store.Read(ctx, kcfg.AdminSecretRef)
	if err != nil {
		return errorResult(ComponentKeycloak, "admin-credentials", err)
	}
	hc, err := httpx.Client(kcfg.CAFile, kcfg.InsecureSkipVerify)
	if err != nil {
		return errorResult(ComponentKeycloak, "http", err)
	}
	kc, err := keycloak.NewReconcilerWithHTTPClient(ctx, kcfg.URL, kcfg.AdminUsername, password, hc)
	if err != nil {
		return errorResult(ComponentKeycloak, "login", err)
	}
	kc.SetDryRun(dryRun)
	return reconcileKeycloak(ctx, kc, cfg, store, dryRun)
}

//nolint:gocognit // a flat sequence of independent steps
func reconcileKeycloak(ctx context.Context, kc *keycloak.Reconciler, cfg *config.Config, store secrets.Store, dryRun bool) []report.Result {
	kcfg := cfg.Keycloak
	k := &kcRun{kc: kc, realm: kcfg.Realm}

	c, err := kc.EnsureRealm(ctx, k.realm)
	k.add("realm", k.realm, c, "", err)
	if err != nil {
		return k.results
	}
	if c == keycloak.ChangeCreated && dryRun {
		k.results = append(k.results, report.Result{
			Component: ComponentKeycloak, Kind: "realm-objects", Name: k.realm, Action: report.ActionSkip,
			Detail: "realm does not exist yet; everything in it would be created",
		})
		return k.results
	}

	if kcfg.BruteForceProtected != nil || kcfg.OTPPolicy != nil {
		settings := keycloak.RealmSettings{BruteForceProtected: kcfg.BruteForceProtected}
		if p := kcfg.OTPPolicy; p != nil {
			settings.OTPPolicy = &keycloak.OTPPolicy{Type: p.Type, Algorithm: p.Algorithm, Digits: p.Digits, Period: p.Period}
		}
		c, detail, err := kc.EnsureRealmSettings(ctx, k.realm, settings)
		k.add("realm-settings", k.realm, c, detail, err)
	}
	if kcfg.RequireTOTP {
		c, err := kc.EnsureRequiredAction(ctx, k.realm, requiredActionTOTP, true, true)
		k.add("required-action", requiredActionTOTP, c, "", err)
	}

	for _, role := range []string{cfg.Operators.AdminRole, cfg.Operators.AnalystRole} {
		if role != "" {
			c, err := kc.EnsureRealmRole(ctx, k.realm, keycloak.RealmRoleSpec{Name: role})
			k.add("realm-role", role, c, "", err)
		}
	}
	if g := cfg.Operators.AnalystGroup; g != "" {
		c, err := kc.EnsureGroup(ctx, k.realm, g)
		k.add("group", g, c, "", err)
	}
	for _, t := range cfg.Tenants {
		g := cfg.GroupName(t)
		c, err := kc.EnsureRealmRole(ctx, k.realm, keycloak.RealmRoleSpec{Name: g})
		k.add("realm-role", g, c, "", err)
		c, err = kc.EnsureGroup(ctx, k.realm, g)
		k.add("group", g, c, "", err)
		c, err = kc.EnsureGroupRealmRoles(ctx, k.realm, g, []string{g})
		k.add("group-realm-roles", g, c, "", err)
	}

	generated := generatedKeys(cfg)
	for _, cl := range cfg.Clients {
		secret, detail, err := clientSecret(ctx, store, cl, generated, dryRun)
		if err != nil {
			k.add(keycloak.KindClient, cl.ClientID, keycloak.ChangeNone, "", err)
			continue
		}
		results, err := kc.EnsureClient(ctx, k.realm, ClientSpec(cl, secret))
		if detail != "" && len(results) > 0 {
			results[0].Detail = joinDetail(results[0].Detail, detail)
		}
		k.addAll(results, err, keycloak.KindClient, cl.ClientID)
	}

	if f := kcfg.MFAFlow; f != nil {
		bind := f.Bind == nil || *f.Bind
		results, err := kc.EnsureBrowserMFAFlow(ctx, k.realm, keycloak.MFAFlowSpec{Alias: f.Alias, CopyFrom: f.CopyFrom, Bind: bind})
		k.addAll(results, err, keycloak.KindAuthFlow, f.Alias)
	}

	for _, t := range cfg.Tenants {
		if t.IdP == nil {
			continue
		}
		spec, err := idpSpec(ctx, store, cfg, t)
		if err != nil {
			k.add(keycloak.KindIdentityProvider, t.IdP.Alias, keycloak.ChangeNone, "", err)
			continue
		}
		results, err := kc.EnsureIdentityProvider(ctx, k.realm, spec)
		k.addAll(results, err, keycloak.KindIdentityProvider, t.IdP.Alias)
	}
	return k.results
}

// clientSecret reads a client's secret. In a dry run a Secret the
// secrets component would create first is not an error.
func clientSecret(
	ctx context.Context, store secrets.Store, cl config.Client, generated map[string]bool, dryRun bool,
) (string, string, error) {
	if cl.SecretRef == nil {
		return "", "", nil
	}
	v, err := store.Read(ctx, *cl.SecretRef)
	if err == nil {
		return v, "", nil
	}
	if dryRun && generated[refKey(*cl.SecretRef)] {
		return "", "secret not generated yet, not compared", nil
	}
	return "", "", fmt.Errorf("client secret: %w", err)
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// ClientSpec maps a config client onto the Keycloak spec.
func ClientSpec(cl config.Client, secret string) keycloak.ManagedClientSpec {
	spec := keycloak.ManagedClientSpec{
		ClientID:                  cl.ClientID,
		Name:                      cl.Name,
		Description:               cl.Description,
		PublicClient:              cl.PublicClient,
		StandardFlowEnabled:       cl.StandardFlowEnabled,
		DirectAccessGrantsEnabled: cl.DirectAccessGrantsEnabled,
		ServiceAccountsEnabled:    cl.ServiceAccountsEnabled,
		FullScopeAllowed:          cl.FullScopeAllowed,
		RootURL:                   cl.RootURL,
		BaseURL:                   cl.BaseURL,
		RedirectURIs:              cl.RedirectURIs,
		WebOrigins:                cl.WebOrigins,
		PostLogoutRedirectURIs:    cl.PostLogoutRedirectURIs,
		Attributes:                cl.Attributes,
		Secret:                    secret,
		DefaultClientScopes:       cl.DefaultClientScopes,
		ServiceAccountClientRoles: cl.ServiceAccountClientRoles,
	}
	for _, m := range cl.ProtocolMappers {
		spec.ProtocolMappers = append(spec.ProtocolMappers, keycloak.ProtocolMapperSpec{
			Name: m.Name, Protocol: "openid-connect", ProtocolMapper: m.ProtocolMapper, Config: m.Config,
		})
	}
	if sm := cl.ScopeMappings; sm != nil {
		spec.ScopeMappingsRealm = sortedCopy(sm.Realm)
		spec.ScopeMappingsClient = sm.Clients
	}
	return spec
}

func idpSpec(ctx context.Context, store secrets.Store, cfg *config.Config, t config.Tenant) (keycloak.IdentityProviderSpec, error) {
	idp := t.IdP
	spec := keycloak.IdentityProviderSpec{
		Alias:       idp.Alias,
		DisplayName: idp.DisplayName,
		ProviderID:  idp.ProviderID,
		Enabled:     idp.Enabled == nil || *idp.Enabled,
		TrustEmail:  idp.TrustEmail,
		Config:      idp.Config,
		Group:       cfg.GroupName(t),
	}
	if idp.ClientSecretRef != nil {
		v, err := store.Read(ctx, *idp.ClientSecretRef)
		if err != nil {
			return spec, fmt.Errorf("identity provider client secret: %w", err)
		}
		spec.ClientSecret = v
	}
	return spec, nil
}
