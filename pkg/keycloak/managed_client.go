// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package keycloak

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Client kinds used in Result.Kind.
const (
	KindClient              = "client"
	KindClientSecret        = "client-secret"
	KindClientMapper        = "client-mapper"
	KindClientDefaultScopes = "client-default-scopes"
	KindClientScopeMappings = "client-scope-mappings"
	KindServiceAccountRoles = "service-account-roles"
)

// attrPostLogoutRedirectURIs is the client attribute holding the
// "##"-separated list of valid post-logout redirect URIs.
const attrPostLogoutRedirectURIs = "post.logout.redirect.uris"

// ManagedClientSpec is the desired state of an OIDC client as managed
// by EnsureClient. Unlike ClientSpec it is compared against an
// existing client and drift is corrected, for the managed fields only:
//
//   - PublicClient, StandardFlowEnabled, DirectAccessGrantsEnabled,
//     ServiceAccountsEnabled: always compared.
//   - Name, Description, RootURL, BaseURL, FullScopeAllowed: compared
//     when set.
//   - RedirectURIs, WebOrigins, PostLogoutRedirectURIs,
//     DefaultClientScopes, scope mappings and service-account roles are
//     supersets: missing entries are added, extra ones are never
//     removed.
//   - Attributes: only the listed keys are compared.
//   - Secret: set on create; when non-empty and the stored secret
//     differs it is updated, so Keycloak matches the Kubernetes Secret
//     the application reads.
type ManagedClientSpec struct {
	ClientID                  string
	Name                      string
	Description               string
	PublicClient              bool
	StandardFlowEnabled       bool
	DirectAccessGrantsEnabled bool
	ServiceAccountsEnabled    bool
	FullScopeAllowed          *bool
	RootURL                   string
	BaseURL                   string
	RedirectURIs              []string
	WebOrigins                []string
	PostLogoutRedirectURIs    []string
	Attributes                map[string]string
	Secret                    string

	DefaultClientScopes []string
	ProtocolMappers     []ProtocolMapperSpec

	// ScopeMappingsRealm / ScopeMappingsClient put realm roles and
	// client roles (keyed by the owning clientId) in the client's
	// scope; needed when FullScopeAllowed is false.
	ScopeMappingsRealm  []string
	ScopeMappingsClient map[string][]string

	// ServiceAccountClientRoles grants client roles (keyed by the
	// owning clientId, e.g. "realm-management") to the client's
	// service-account user.
	ServiceAccountClientRoles map[string][]string
}

// EnsureClient reconciles one client and everything hanging off it.
// It returns one Result per sub-object so callers can report drift
// precisely. On the first error it stops and returns what it has.
func (r *Reconciler) EnsureClient(ctx context.Context, realm string, spec ManagedClientSpec) ([]Result, error) {
	var results []Result
	add := func(kind, name string, c Change, detail string) {
		results = append(results, Result{Kind: kind, Name: name, Change: c, Detail: detail})
	}

	cur, err := r.getClientRaw(ctx, realm, spec.ClientID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		if r.dryRun {
			// Everything below would be created with the client.
			add(KindClient, spec.ClientID, ChangeCreated, "")
			return results, nil
		}
		if cur, err = r.createClient(ctx, realm, spec); err != nil {
			return nil, err
		}
		add(KindClient, spec.ClientID, ChangeCreated, "")
	} else {
		change, detail, err := r.updateClientSettings(ctx, realm, cur, spec)
		if err != nil {
			return results, err
		}
		add(KindClient, spec.ClientID, change, detail)
	}
	id := asString(cur["id"])

	if spec.Secret != "" && !spec.PublicClient {
		change, err := r.ensureClientSecret(ctx, realm, id, spec.Secret)
		if err != nil {
			return results, err
		}
		add(KindClientSecret, spec.ClientID, change, "")
	}

	for _, m := range spec.ProtocolMappers {
		change, err := r.EnsureClientProtocolMapper(ctx, realm, id, m)
		if err != nil {
			return results, err
		}
		add(KindClientMapper, spec.ClientID+"/"+m.Name, change, "")
	}

	if len(spec.DefaultClientScopes) > 0 {
		change, detail, err := r.ensureClientDefaultScopes(ctx, realm, id, cur, spec.DefaultClientScopes)
		if err != nil {
			return results, err
		}
		add(KindClientDefaultScopes, spec.ClientID, change, detail)
	}

	if len(spec.ScopeMappingsRealm) > 0 || len(spec.ScopeMappingsClient) > 0 {
		change, detail, err := r.ensureClientScopeMappings(ctx, realm, id, spec)
		if err != nil {
			return results, err
		}
		add(KindClientScopeMappings, spec.ClientID, change, detail)
	}

	if len(spec.ServiceAccountClientRoles) > 0 {
		change, detail, err := r.ensureServiceAccountClientRoles(ctx, realm, id, spec)
		if err != nil {
			return results, err
		}
		add(KindServiceAccountRoles, spec.ClientID, change, detail)
	}
	return results, nil
}

func (r *Reconciler) createClient(ctx context.Context, realm string, spec ManagedClientSpec) (map[string]any, error) {
	if err := r.do(ctx, methodPost, realmPath(realm, "/clients"), newClientBody(spec), nil); err != nil {
		return nil, fmt.Errorf("creating client %q in realm %q: %w", spec.ClientID, realm, err)
	}
	cur, err := r.getClientRaw(ctx, realm, spec.ClientID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, fmt.Errorf("client %q not found after create", spec.ClientID)
	}
	return cur, nil
}

// getClientRaw returns the full client representation, or nil.
func (r *Reconciler) getClientRaw(ctx context.Context, realm, clientID string) (map[string]any, error) {
	q := url.Values{}
	q.Set("clientId", clientID)
	var list []map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, "/clients?"+q.Encode()), nil, &list); err != nil {
		return nil, fmt.Errorf("listing clients in realm %q: %w", realm, err)
	}
	for _, c := range list {
		if asString(c["clientId"]) == clientID {
			return c, nil
		}
	}
	return nil, nil
}

func (r *Reconciler) clientInternalID(ctx context.Context, realm, clientID string) (string, error) {
	c, err := r.getClientRaw(ctx, realm, clientID)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", nil
	}
	return asString(c["id"]), nil
}

func newClientBody(spec ManagedClientSpec) map[string]any {
	body := map[string]any{
		"clientId":                  spec.ClientID,
		keyEnabled:                  true,
		"protocol":                  protocolOIDC,
		"publicClient":              spec.PublicClient,
		"standardFlowEnabled":       spec.StandardFlowEnabled,
		"directAccessGrantsEnabled": spec.DirectAccessGrantsEnabled,
		"serviceAccountsEnabled":    spec.ServiceAccountsEnabled,
		"implicitFlowEnabled":       false,
	}
	if !spec.PublicClient {
		body["clientAuthenticatorType"] = "client-secret"
		if spec.Secret != "" {
			body["secret"] = spec.Secret
		}
	}
	setIf := func(key, v string) {
		if v != "" {
			body[key] = v
		}
	}
	setIf(keyName, spec.Name)
	setIf("description", spec.Description)
	setIf("rootUrl", spec.RootURL)
	setIf("baseUrl", spec.BaseURL)
	if spec.FullScopeAllowed != nil {
		body["fullScopeAllowed"] = *spec.FullScopeAllowed
	}
	if len(spec.RedirectURIs) > 0 {
		body["redirectUris"] = spec.RedirectURIs
	}
	if len(spec.WebOrigins) > 0 {
		body["webOrigins"] = spec.WebOrigins
	}
	attrs := map[string]any{}
	for k, v := range spec.Attributes {
		attrs[k] = v
	}
	if len(spec.PostLogoutRedirectURIs) > 0 {
		attrs[attrPostLogoutRedirectURIs] = strings.Join(spec.PostLogoutRedirectURIs, "##")
	}
	if len(attrs) > 0 {
		body["attributes"] = attrs
	}
	return body
}

// updateClientSettings diffs the managed top-level fields and PUTs
// the existing representation back with only those changed. The
// secret and protocol mappers are stripped from the body so the PUT
// leaves them alone.
//
//nolint:gocognit // a flat list of per-field comparisons
func (r *Reconciler) updateClientSettings(
	ctx context.Context, realm string, cur map[string]any, spec ManagedClientSpec,
) (Change, string, error) {
	var drift []string
	setBool := func(key string, want bool) {
		if asBool(cur[key]) != want {
			cur[key] = want
			drift = append(drift, key)
		}
	}
	setString := func(key, want string) {
		if want != "" && asString(cur[key]) != want {
			cur[key] = want
			drift = append(drift, key)
		}
	}
	superset := func(key string, want []string) {
		have := asStrings(cur[key])
		if add := missing(have, want); len(add) > 0 {
			cur[key] = append(have, add...)
			drift = append(drift, key+"+="+strings.Join(add, " "))
		}
	}

	setBool("publicClient", spec.PublicClient)
	setBool("standardFlowEnabled", spec.StandardFlowEnabled)
	setBool("directAccessGrantsEnabled", spec.DirectAccessGrantsEnabled)
	setBool("serviceAccountsEnabled", spec.ServiceAccountsEnabled)
	if spec.FullScopeAllowed != nil {
		setBool("fullScopeAllowed", *spec.FullScopeAllowed)
	}
	setString(keyName, spec.Name)
	setString("description", spec.Description)
	setString("rootUrl", spec.RootURL)
	setString("baseUrl", spec.BaseURL)
	superset("redirectUris", spec.RedirectURIs)
	superset("webOrigins", spec.WebOrigins)

	attrs := asMap(cur["attributes"])
	for _, k := range sortedKeys(spec.Attributes) {
		if asString(attrs[k]) != spec.Attributes[k] {
			attrs[k] = spec.Attributes[k]
			drift = append(drift, "attributes."+k)
		}
	}
	if len(spec.PostLogoutRedirectURIs) > 0 {
		have := splitHashes(asString(attrs[attrPostLogoutRedirectURIs]))
		if add := missing(have, spec.PostLogoutRedirectURIs); len(add) > 0 {
			attrs[attrPostLogoutRedirectURIs] = strings.Join(append(have, add...), "##")
			drift = append(drift, "attributes."+attrPostLogoutRedirectURIs+"+="+strings.Join(add, " "))
		}
	}
	cur["attributes"] = attrs

	if len(drift) == 0 {
		return ChangeNone, "", nil
	}
	detail := strings.Join(drift, ",")
	if r.dryRun {
		return ChangeUpdated, detail, nil
	}
	body := make(map[string]any, len(cur))
	for k, v := range cur {
		if k == "secret" || k == "protocolMappers" {
			continue
		}
		body[k] = v
	}
	id := asString(cur["id"])
	if err := r.do(ctx, methodPut, realmPath(realm, "/clients/"+url.PathEscape(id)), body, nil); err != nil {
		return ChangeNone, "", fmt.Errorf("updating client %q (%s): %w", spec.ClientID, detail, err)
	}
	return ChangeUpdated, detail, nil
}

func splitHashes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "##") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ensureClientSecret compares the stored secret with want (never
// printing either) and sets it when they differ.
func (r *Reconciler) ensureClientSecret(ctx context.Context, realm, id, want string) (Change, error) {
	var cred map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, "/clients/"+url.PathEscape(id)+"/client-secret"), nil, &cred); err != nil {
		return ChangeNone, fmt.Errorf("reading client secret: %w", err)
	}
	if asString(cred["value"]) == want {
		return ChangeNone, nil
	}
	if r.dryRun {
		return ChangeUpdated, nil
	}
	var client map[string]any
	path := realmPath(realm, "/clients/"+url.PathEscape(id))
	if err := r.do(ctx, methodGet, path, nil, &client); err != nil {
		return ChangeNone, fmt.Errorf("reading client: %w", err)
	}
	delete(client, "protocolMappers")
	client["secret"] = want
	if err := r.do(ctx, methodPut, path, client, nil); err != nil {
		return ChangeNone, fmt.Errorf("setting client secret: %w", err)
	}
	return ChangeUpdated, nil
}

// EnsureClientProtocolMapper makes sure a protocol mapper named
// spec.Name exists directly on the client (not on a client scope),
// with the mapper type and every config key the spec sets. Keys the
// spec does not set are left alone.
func (r *Reconciler) EnsureClientProtocolMapper(ctx context.Context, realm, clientInternalID string, spec ProtocolMapperSpec) (Change, error) {
	base := realmPath(realm, "/clients/"+url.PathEscape(clientInternalID)+"/protocol-mappers/models")
	var list []map[string]any
	if err := r.do(ctx, methodGet, base, nil, &list); err != nil {
		return ChangeNone, fmt.Errorf("listing protocol mappers: %w", err)
	}
	protocol := spec.Protocol
	if protocol == "" {
		protocol = protocolOIDC
	}
	for _, m := range list {
		if asString(m[keyName]) != spec.Name {
			continue
		}
		cfg := asMap(m[keyConfig])
		drift := asString(m["protocolMapper"]) != spec.ProtocolMapper
		for k, v := range spec.Config {
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
		m["protocolMapper"] = spec.ProtocolMapper
		m[keyConfig] = cfg
		if err := r.do(ctx, methodPut, base+"/"+url.PathEscape(asString(m["id"])), m, nil); err != nil {
			return ChangeNone, fmt.Errorf("updating protocol mapper %q: %w", spec.Name, err)
		}
		return ChangeUpdated, nil
	}
	if r.dryRun {
		return ChangeCreated, nil
	}
	body := map[string]any{
		keyName:          spec.Name,
		"protocol":       protocol,
		"protocolMapper": spec.ProtocolMapper,
		keyConfig:        spec.Config,
	}
	if err := r.do(ctx, methodPost, base, body, nil); err != nil {
		return ChangeNone, fmt.Errorf("creating protocol mapper %q: %w", spec.Name, err)
	}
	return ChangeCreated, nil
}

func (r *Reconciler) ensureClientDefaultScopes(
	ctx context.Context, realm, id string, cur map[string]any, want []string,
) (Change, string, error) {
	add := missing(asStrings(cur["defaultClientScopes"]), want)
	if len(add) == 0 {
		return ChangeNone, "", nil
	}
	detail := strings.Join(add, ",")
	if r.dryRun {
		return ChangeUpdated, detail, nil
	}
	var scopes []map[string]any
	if err := r.do(ctx, methodGet, realmPath(realm, "/client-scopes"), nil, &scopes); err != nil {
		return ChangeNone, "", fmt.Errorf("listing client scopes: %w", err)
	}
	for _, name := range add {
		scopeID := ""
		for _, s := range scopes {
			if asString(s[keyName]) == name {
				scopeID = asString(s["id"])
			}
		}
		if scopeID == "" {
			return ChangeNone, "", fmt.Errorf("client scope %q not found in realm %q", name, realm)
		}
		path := realmPath(realm, "/clients/"+url.PathEscape(id)+"/default-client-scopes/"+url.PathEscape(scopeID))
		if err := r.do(ctx, methodPut, path, nil, nil); err != nil {
			return ChangeNone, "", fmt.Errorf("adding default scope %q: %w", name, err)
		}
	}
	return ChangeUpdated, detail, nil
}

func (r *Reconciler) ensureClientScopeMappings(ctx context.Context, realm, id string, spec ManagedClientSpec) (Change, string, error) {
	var drift []string
	base := realmPath(realm, "/clients/"+url.PathEscape(id)+"/scope-mappings")

	if len(spec.ScopeMappingsRealm) > 0 {
		add, err := r.missingRoles(ctx, base+"/realm", spec.ScopeMappingsRealm)
		if err != nil {
			return ChangeNone, "", fmt.Errorf("reading realm scope mappings of %q: %w", spec.ClientID, err)
		}
		if len(add) > 0 {
			drift = append(drift, "realm:"+strings.Join(add, "+"))
			if err := r.addRealmRoles(ctx, realm, base+"/realm", add); err != nil {
				return ChangeNone, "", fmt.Errorf("adding realm scope mappings to %q: %w", spec.ClientID, err)
			}
		}
	}

	d, err := r.addClientRolesAt(ctx, realm, spec.ScopeMappingsClient, func(ownerID string) string {
		return base + "/clients/" + url.PathEscape(ownerID)
	})
	if err != nil {
		return ChangeNone, "", fmt.Errorf("client scope mappings of %q: %w", spec.ClientID, err)
	}
	drift = append(drift, d...)
	if len(drift) == 0 {
		return ChangeNone, "", nil
	}
	return ChangeUpdated, strings.Join(drift, ","), nil
}

// missingRoles GETs a role-mapping endpoint and returns the wanted
// role names it lacks.
func (r *Reconciler) missingRoles(ctx context.Context, path string, want []string) ([]string, error) {
	var cur []map[string]any
	if err := r.do(ctx, methodGet, path, nil, &cur); err != nil {
		return nil, err
	}
	return missing(roleNames(cur), want), nil
}

func (r *Reconciler) addRealmRoles(ctx context.Context, realm, path string, names []string) error {
	if r.dryRun {
		return nil
	}
	refs, err := r.realmRoleRefs(ctx, realm, names)
	if err != nil {
		return err
	}
	return r.do(ctx, methodPost, path, refs, nil)
}

// addClientRolesAt adds, per owning client, the missing client roles
// at the role-mapping endpoint pathFor(ownerInternalID) returns.
func (r *Reconciler) addClientRolesAt(
	ctx context.Context, realm string, roles map[string][]string, pathFor func(ownerID string) string,
) ([]string, error) {
	var drift []string
	for _, owner := range sortedKeys(roles) {
		ownerID, err := r.clientInternalID(ctx, realm, owner)
		if err != nil {
			return nil, err
		}
		if ownerID == "" {
			return nil, fmt.Errorf("client %q not found in realm %q", owner, realm)
		}
		path := pathFor(ownerID)
		add, err := r.missingRoles(ctx, path, roles[owner])
		if err != nil {
			return nil, fmt.Errorf("reading %s roles: %w", owner, err)
		}
		if len(add) == 0 {
			continue
		}
		drift = append(drift, owner+":"+strings.Join(add, "+"))
		if r.dryRun {
			continue
		}
		refs, err := r.clientRoleRefs(ctx, realm, ownerID, owner, add)
		if err != nil {
			return nil, err
		}
		if err := r.do(ctx, methodPost, path, refs, nil); err != nil {
			return nil, fmt.Errorf("adding %s roles: %w", owner, err)
		}
	}
	return drift, nil
}

func (r *Reconciler) ensureServiceAccountClientRoles(ctx context.Context, realm, id string, spec ManagedClientSpec) (Change, string, error) {
	if r.dryRun && !spec.ServiceAccountsEnabled {
		return ChangeNone, "", fmt.Errorf("client %q has service-account roles but serviceAccountsEnabled is false", spec.ClientID)
	}
	var sa map[string]any
	err := r.do(ctx, methodGet, realmPath(realm, "/clients/"+url.PathEscape(id)+"/service-account-user"), nil, &sa)
	if err != nil {
		if r.dryRun {
			// Service accounts are switched on by the settings update
			// reported above; the user does not exist yet.
			return ChangeUpdated, "service account pending", nil
		}
		return ChangeNone, "", fmt.Errorf("reading service-account user of %q: %w", spec.ClientID, err)
	}
	userID := asString(sa["id"])

	drift, err := r.addClientRolesAt(ctx, realm, spec.ServiceAccountClientRoles, func(ownerID string) string {
		return realmPath(realm, "/users/"+url.PathEscape(userID)+"/role-mappings/clients/"+url.PathEscape(ownerID))
	})
	if err != nil {
		return ChangeNone, "", fmt.Errorf("service-account roles of %q: %w", spec.ClientID, err)
	}
	if len(drift) == 0 {
		return ChangeNone, "", nil
	}
	return ChangeUpdated, strings.Join(drift, ","), nil
}

func (r *Reconciler) clientRoleRefs(ctx context.Context, realm, ownerID, owner string, names []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		var role map[string]any
		path := realmPath(realm, "/clients/"+url.PathEscape(ownerID)+"/roles/"+url.PathEscape(n))
		if err := r.do(ctx, methodGet, path, nil, &role); err != nil {
			return nil, fmt.Errorf("looking up role %q of client %q: %w", n, owner, err)
		}
		out = append(out, map[string]any{"id": role["id"], keyName: role[keyName], "clientRole": true})
	}
	return out, nil
}

func roleNames(list []map[string]any) []string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, asString(m[keyName]))
	}
	sort.Strings(out)
	return out
}
