// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package config defines the siem-reconciler input: the tenants.json
// file the security-operations Helm chart renders into a ConfigMap
// (mounted at /etc/siem/tenants.json). See README.md in this directory
// for the field reference and testdata/tenants.example.json for a
// complete example. The file carries secret references only, never
// secret values.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Defaults applied by Load.
const (
	DefaultTenantGroupPrefix = "tenant-"
	DefaultKeycloakAdmin     = "admin"
	DefaultMFAFlowAlias      = "browser-mfa"
	DefaultMFAFlowCopyFrom   = "browser"
	DefaultIRISInitialCust   = "IrisInitialClient"
	DefaultWazuhUsernameKey  = "API_USERNAME"
	DefaultWazuhPasswordKey  = "API_PASSWORD"
	DefaultWazuhAdminRole    = "administrator"
	DefaultWazuhAnalystRole  = "readonly"
	DefaultVeloAPIClientKey  = "api_client.yaml"
	DefaultIdPProvider       = "oidc"
)

// Config is the root of tenants.json.
type Config struct {
	// Domain is the base DNS domain of the stack (informational; URLs
	// in clients are given in full).
	Domain string `json:"domain"`
	// TenantGroupPrefix prefixes a tenant code to form its Keycloak
	// group, realm role, Wazuh agent group and Wazuh RBAC names.
	TenantGroupPrefix string `json:"tenantGroupPrefix,omitempty"`

	Keycloak   Keycloak          `json:"keycloak"`
	Operators  Operators         `json:"operators"`
	Tenants    []Tenant          `json:"tenants"`
	Clients    []Client          `json:"clients,omitempty"`
	Secrets    []GeneratedSecret `json:"secrets,omitempty"`
	Components Components        `json:"components"`
}

// SecretRef points at one key of a Kubernetes Secret.
type SecretRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Key       string `json:"key"`
}

// Keycloak holds the realm-level settings.
type Keycloak struct {
	// URL is Keycloak's HTTP root including the context path, e.g.
	// http://keycloakx-http.keycloakx.svc/auth.
	URL   string `json:"url"`
	Realm string `json:"realm"`
	// AdminUsername defaults to "admin"; AdminSecretRef holds its
	// password (master realm).
	AdminUsername  string    `json:"adminUsername,omitempty"`
	AdminSecretRef SecretRef `json:"adminSecretRef"`

	CAFile             string `json:"caFile,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`

	// BruteForceProtected / OTPPolicy are left alone when omitted.
	BruteForceProtected *bool      `json:"bruteForceProtected,omitempty"`
	OTPPolicy           *OTPPolicy `json:"otpPolicy,omitempty"`
	// RequireTOTP enables CONFIGURE_TOTP as a default required action.
	RequireTOTP bool `json:"requireTOTP,omitempty"`
	// MFAFlow, when set, makes OTP mandatory in the browser flow.
	MFAFlow *MFAFlow `json:"mfaFlow,omitempty"`
}

// OTPPolicy mirrors Keycloak's realm OTP policy fields.
type OTPPolicy struct {
	Type      string `json:"type"`
	Algorithm string `json:"algorithm"`
	Digits    int    `json:"digits"`
	Period    int    `json:"period"`
}

// MFAFlow names the managed browser flow.
type MFAFlow struct {
	Alias    string `json:"alias,omitempty"`
	CopyFrom string `json:"copyFrom,omitempty"`
	// Bind defaults to true.
	Bind *bool `json:"bind,omitempty"`
}

// Operators are the SOC staff roles: realm roles for administrators
// and analysts, and the group analysts are put in.
type Operators struct {
	AdminRole    string `json:"adminRole"`
	AnalystRole  string `json:"analystRole"`
	AnalystGroup string `json:"analystGroup"`
}

// Tenant is one customer organisation.
type Tenant struct {
	// Code is the short, stable id: lowercase letters, digits and
	// dashes (e.g. "001"). The group name is TenantGroupPrefix+Code.
	Code string `json:"code"`
	// Name is the display name; also the IRIS customer and the
	// Velociraptor org name.
	Name string `json:"name"`
	// RetentionDays is the event retention for the tenant's indices
	// (consumed by the chart's index policies; not applied here).
	RetentionDays int `json:"retentionDays,omitempty"`
	// IdP optionally brokers the tenant's own identity provider.
	IdP *IdP `json:"idp,omitempty"`
}

// IdP is an identity-provider broker whose users land in the tenant
// group.
type IdP struct {
	Alias       string            `json:"alias,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	ProviderID  string            `json:"providerId,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	TrustEmail  bool              `json:"trustEmail,omitempty"`
	Config      map[string]string `json:"config"`
	// ClientSecretRef is read on create only.
	ClientSecretRef *SecretRef `json:"clientSecretRef,omitempty"`
}

// Client is a Keycloak OIDC client. Lists are supersets: entries are
// added, never removed.
type Client struct {
	ClientID                  string              `json:"clientId"`
	Name                      string              `json:"name,omitempty"`
	Description               string              `json:"description,omitempty"`
	PublicClient              bool                `json:"publicClient,omitempty"`
	StandardFlowEnabled       bool                `json:"standardFlowEnabled,omitempty"`
	DirectAccessGrantsEnabled bool                `json:"directAccessGrantsEnabled,omitempty"`
	ServiceAccountsEnabled    bool                `json:"serviceAccountsEnabled,omitempty"`
	FullScopeAllowed          *bool               `json:"fullScopeAllowed,omitempty"`
	RootURL                   string              `json:"rootUrl,omitempty"`
	BaseURL                   string              `json:"baseUrl,omitempty"`
	RedirectURIs              []string            `json:"redirectUris,omitempty"`
	WebOrigins                []string            `json:"webOrigins,omitempty"`
	PostLogoutRedirectURIs    []string            `json:"postLogoutRedirectUris,omitempty"`
	Attributes                map[string]string   `json:"attributes,omitempty"`
	DefaultClientScopes       []string            `json:"defaultClientScopes,omitempty"`
	ProtocolMappers           []ProtocolMapper    `json:"protocolMappers,omitempty"`
	ScopeMappings             *ScopeMappings      `json:"scopeMappings,omitempty"`
	ServiceAccountClientRoles map[string][]string `json:"serviceAccountClientRoles,omitempty"`
	// SecretRef holds the confidential client's secret; Keycloak is
	// made to match it. Omit to leave the secret alone.
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

// ProtocolMapper is a mapper attached directly to a client.
type ProtocolMapper struct {
	Name           string            `json:"name"`
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config"`
}

// ScopeMappings lists realm roles and client roles (keyed by owning
// clientId) in a client's scope.
type ScopeMappings struct {
	Realm   []string            `json:"realm,omitempty"`
	Clients map[string][]string `json:"clients,omitempty"`
}

// GeneratedSecret is a Kubernetes Secret created with random values
// when missing. Existing values are never changed.
type GeneratedSecret struct {
	Namespace string         `json:"namespace"`
	Name      string         `json:"name"`
	Keys      []GeneratedKey `json:"keys"`
}

// Generators for GeneratedKey.
const (
	GeneratorPassword = "password"  // 32 alphanumeric characters
	GeneratorHex32    = "hex32"     // 32 random bytes, hex encoded
	GeneratorBase64   = "base64-32" // 32 random bytes, base64 encoded
)

// GeneratedKey is one key of a GeneratedSecret.
type GeneratedKey struct {
	Key       string `json:"key"`
	Generator string `json:"generator,omitempty"`
}

// Components holds the per-application API settings; a nil component
// is skipped.
type Components struct {
	IRIS         *IRIS         `json:"iris,omitempty"`
	Wazuh        *Wazuh        `json:"wazuh,omitempty"`
	Velociraptor *Velociraptor `json:"velociraptor,omitempty"`
}

// IRIS is DFIR-IRIS.
type IRIS struct {
	URL                string    `json:"url"`
	APIKeySecretRef    SecretRef `json:"apiKeySecretRef"`
	CAFile             string    `json:"caFile,omitempty"`
	InsecureSkipVerify bool      `json:"insecureSkipVerify,omitempty"`
	// InitialCustomer is IRIS's built-in customer, added to every
	// service account's customer list next to the tenants.
	InitialCustomer string `json:"initialCustomer,omitempty"`
	// ServiceAccounts get these IRIS groups and every tenant's
	// customer. The accounts themselves are not created.
	ServiceAccounts []IRISServiceAccount `json:"serviceAccounts,omitempty"`
}

// IRISServiceAccount is an existing IRIS login.
type IRISServiceAccount struct {
	Login  string   `json:"login"`
	Groups []string `json:"groups,omitempty"`
}

// Wazuh is the Wazuh manager API.
type Wazuh struct {
	URL                string        `json:"url"`
	CredSecretRef      WazuhCredsRef `json:"credSecretRef"`
	CAFile             string        `json:"caFile,omitempty"`
	InsecureSkipVerify bool          `json:"insecureSkipVerify,omitempty"`
	// AdminAPIRole / AnalystAPIRole are the Wazuh API roles the
	// operator realm roles map to.
	AdminAPIRole   string `json:"adminApiRole,omitempty"`
	AnalystAPIRole string `json:"analystApiRole,omitempty"`
	// CreateGroups creates missing agent groups through the API.
	CreateGroups bool `json:"createGroups,omitempty"`
}

// WazuhCredsRef points at the Wazuh API user's Secret.
type WazuhCredsRef struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	UsernameKey string `json:"usernameKey,omitempty"`
	PasswordKey string `json:"passwordKey,omitempty"`
}

// Velociraptor is the Velociraptor gRPC API.
type Velociraptor struct {
	// APIClientSecretRef holds an api_client YAML (from
	// `velociraptor config api_client`); APIClientFile is a local
	// alternative. One of them is required.
	APIClientSecretRef *SecretRef `json:"apiClientSecretRef,omitempty"`
	APIClientFile      string     `json:"apiClientFile,omitempty"`
	// Address overrides api_connection_string (e.g. a port-forward).
	Address string `json:"address,omitempty"`
	// ServerMonitoring lists server event artifacts that must be
	// running; others are never removed.
	ServerMonitoring []MonitoredArtifact `json:"serverMonitoring,omitempty"`
}

// MonitoredArtifact is one server monitoring entry. Only the listed
// parameters are compared.
type MonitoredArtifact struct {
	Artifact   string            `json:"artifact"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// GroupName returns the tenant's group / role name.
func (c *Config) GroupName(t Tenant) string { return c.TenantGroupPrefix + t.Code }

// Load reads, defaults and validates a tenants.json file. Unknown
// fields are rejected so a chart/reconciler schema mismatch fails
// loudly instead of being ignored.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse is Load for in-memory data.
func Parse(raw []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing tenants config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.TenantGroupPrefix == "" {
		c.TenantGroupPrefix = DefaultTenantGroupPrefix
	}
	if c.Keycloak.AdminUsername == "" {
		c.Keycloak.AdminUsername = DefaultKeycloakAdmin
	}
	if f := c.Keycloak.MFAFlow; f != nil {
		f.Alias = orDefault(f.Alias, DefaultMFAFlowAlias)
		f.CopyFrom = orDefault(f.CopyFrom, DefaultMFAFlowCopyFrom)
	}
	for i := range c.Tenants {
		if idp := c.Tenants[i].IdP; idp != nil {
			idp.Alias = orDefault(idp.Alias, c.GroupName(c.Tenants[i])+"-idp")
			idp.DisplayName = orDefault(idp.DisplayName, c.Tenants[i].Name)
			idp.ProviderID = orDefault(idp.ProviderID, DefaultIdPProvider)
		}
	}
	for i := range c.Secrets {
		for j := range c.Secrets[i].Keys {
			k := &c.Secrets[i].Keys[j]
			k.Generator = orDefault(k.Generator, GeneratorPassword)
		}
	}
	c.applyComponentDefaults()
}

func (c *Config) applyComponentDefaults() {
	if iris := c.Components.IRIS; iris != nil {
		iris.InitialCustomer = orDefault(iris.InitialCustomer, DefaultIRISInitialCust)
	}
	if w := c.Components.Wazuh; w != nil {
		w.CredSecretRef.UsernameKey = orDefault(w.CredSecretRef.UsernameKey, DefaultWazuhUsernameKey)
		w.CredSecretRef.PasswordKey = orDefault(w.CredSecretRef.PasswordKey, DefaultWazuhPasswordKey)
		w.AdminAPIRole = orDefault(w.AdminAPIRole, DefaultWazuhAdminRole)
		w.AnalystAPIRole = orDefault(w.AnalystAPIRole, DefaultWazuhAnalystRole)
	}
	if v := c.Components.Velociraptor; v != nil && v.APIClientSecretRef != nil {
		v.APIClientSecretRef.Key = orDefault(v.APIClientSecretRef.Key, DefaultVeloAPIClientKey)
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

var (
	codePattern   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	prefixPattern = regexp.MustCompile(`^[a-z0-9-]*$`)
)

// Validate checks the config for errors that would make a run
// meaningless or dangerous. All problems are reported together.
func (c *Config) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Domain == "" {
		fail("domain is required")
	}
	if !prefixPattern.MatchString(c.TenantGroupPrefix) {
		fail("tenantGroupPrefix %q may only hold lowercase letters, digits and dashes", c.TenantGroupPrefix)
	}
	if c.Keycloak.URL == "" || c.Keycloak.Realm == "" {
		fail("keycloak.url and keycloak.realm are required")
	}
	validateRef(fail, "keycloak.adminSecretRef", c.Keycloak.AdminSecretRef)
	c.validateTenants(fail)
	c.validateClients(fail)
	c.validateSecrets(fail)
	c.validateComponents(fail)
	return errors.Join(errs...)
}

func (c *Config) validateTenants(fail func(string, ...any)) {
	codes, names := map[string]bool{}, map[string]bool{}
	for i, t := range c.Tenants {
		where := fmt.Sprintf("tenants[%d]", i)
		switch {
		case !codePattern.MatchString(t.Code):
			fail("%s.code %q must be lowercase letters, digits and dashes", where, t.Code)
		case codes[t.Code]:
			fail("%s.code %q is not unique", where, t.Code)
		}
		codes[t.Code] = true
		switch {
		case strings.TrimSpace(t.Name) == "":
			fail("%s.name is required", where)
		case names[t.Name]:
			fail("%s.name %q is not unique", where, t.Name)
		}
		names[t.Name] = true
		if t.RetentionDays < 0 {
			fail("%s.retentionDays must not be negative", where)
		}
		if t.IdP != nil && t.IdP.ClientSecretRef != nil {
			validateRef(fail, where+".idp.clientSecretRef", *t.IdP.ClientSecretRef)
		}
	}
}

func (c *Config) validateClients(fail func(string, ...any)) {
	clientIDs := map[string]bool{}
	for i, cl := range c.Clients {
		where := fmt.Sprintf("clients[%d]", i)
		if cl.ClientID == "" {
			fail("%s.clientId is required", where)
		} else if clientIDs[cl.ClientID] {
			fail("%s.clientId %q is not unique", where, cl.ClientID)
		}
		clientIDs[cl.ClientID] = true
		if cl.PublicClient && (cl.ServiceAccountsEnabled || cl.SecretRef != nil) {
			fail("%s: a public client has no secret and no service account", where)
		}
		if cl.SecretRef != nil {
			validateRef(fail, where+".secretRef", *cl.SecretRef)
		}
		if len(cl.ServiceAccountClientRoles) > 0 && !cl.ServiceAccountsEnabled {
			fail("%s: serviceAccountClientRoles needs serviceAccountsEnabled", where)
		}
		for j, m := range cl.ProtocolMappers {
			if m.Name == "" || m.ProtocolMapper == "" {
				fail("%s.protocolMappers[%d] needs name and protocolMapper", where, j)
			}
		}
	}
}

func (c *Config) validateSecrets(fail func(string, ...any)) {
	for i, s := range c.Secrets {
		where := fmt.Sprintf("secrets[%d]", i)
		if s.Namespace == "" || s.Name == "" || len(s.Keys) == 0 {
			fail("%s needs namespace, name and at least one key", where)
		}
		for j, k := range s.Keys {
			switch k.Generator {
			case GeneratorPassword, GeneratorHex32, GeneratorBase64:
			default:
				fail("%s.keys[%d].generator %q is not one of password, hex32, base64-32", where, j, k.Generator)
			}
			if k.Key == "" {
				fail("%s.keys[%d].key is required", where, j)
			}
		}
	}
}

func (c *Config) validateComponents(fail func(string, ...any)) {
	if iris := c.Components.IRIS; iris != nil {
		if iris.URL == "" {
			fail("components.iris.url is required")
		}
		validateRef(fail, "components.iris.apiKeySecretRef", iris.APIKeySecretRef)
		for i, sa := range iris.ServiceAccounts {
			if sa.Login == "" {
				fail("components.iris.serviceAccounts[%d].login is required", i)
			}
		}
	}
	if w := c.Components.Wazuh; w != nil {
		if w.URL == "" {
			fail("components.wazuh.url is required")
		}
		if w.CredSecretRef.Namespace == "" || w.CredSecretRef.Name == "" {
			fail("components.wazuh.credSecretRef needs namespace and name")
		}
	}
	if v := c.Components.Velociraptor; v != nil {
		if (v.APIClientSecretRef == nil) == (v.APIClientFile == "") {
			fail("components.velociraptor needs exactly one of apiClientSecretRef and apiClientFile")
		}
		if v.APIClientSecretRef != nil {
			validateRef(fail, "components.velociraptor.apiClientSecretRef", *v.APIClientSecretRef)
		}
		for i, a := range v.ServerMonitoring {
			if a.Artifact == "" {
				fail("components.velociraptor.serverMonitoring[%d].artifact is required", i)
			}
		}
	}
}

func validateRef(fail func(string, ...any), where string, ref SecretRef) {
	if ref.Namespace == "" || ref.Name == "" || ref.Key == "" {
		fail("%s needs namespace, name and key", where)
	}
}
