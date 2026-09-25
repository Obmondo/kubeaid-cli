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
	DefaultWazuhTenantRole   = "readonly"
	DefaultIndexerUserKey    = "INDEXER_USERNAME"
	DefaultIndexerPassKey    = "INDEXER_PASSWORD"
	DefaultWazuhAgentVersion = "4.14.8-1"
	DefaultVeloAPIClientKey  = "api_client.yaml"
	DefaultIdPProvider       = "oidc"
)

// Config is the root of tenants.json.
type Config struct {
	// Domain is the base DNS domain of the stack (informational; URLs
	// in clients are given in full).
	Domain string `json:"domain"`
	// TenantGroupPrefix prefixes a tenant code to form its Keycloak
	// group and realm role, which is also the OpenSearch backend role
	// the tenant's Wazuh manager maps to its tenant API role.
	TenantGroupPrefix string `json:"tenantGroupPrefix,omitempty"`

	Keycloak  Keycloak          `json:"keycloak"`
	Operators Operators         `json:"operators"`
	Tenants   []Tenant          `json:"tenants"`
	Clients   []Client          `json:"clients,omitempty"`
	Secrets   []GeneratedSecret `json:"secrets,omitempty"`
	// SecretCopies copy single Secret keys between namespaces.
	SecretCopies []SecretCopy `json:"secretCopies,omitempty"`
	Components   Components   `json:"components"`
	// Enrolment lists the per-tenant agent enrolment bundle Secrets.
	Enrolment []EnrolmentBundle `json:"enrolment,omitempty"`
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
	// Value, when set, is the literal value the key is created with
	// (e.g. an OIDC client id next to its generated secret); Generator
	// is then ignored.
	Value string `json:"value,omitempty"`
}

// SecretCopy copies one Secret key to another Secret, typically into
// a tenant namespace. The target key is created or overwritten when it
// differs from the source.
type SecretCopy struct {
	From SecretRef `json:"from"`
	To   SecretRef `json:"to"`
}

// Components holds the per-application API settings; a nil component
// is skipped.
type Components struct {
	IRIS *IRIS `json:"iris,omitempty"`
	// Wazuh lists the Wazuh managers, one per tenant.
	Wazuh []Wazuh `json:"wazuh,omitempty"`
	// WazuhCentral is the central search-only indexer that reaches the
	// tenants' indexers through cross-cluster search.
	WazuhCentral *WazuhCentral `json:"wazuhCentral,omitempty"`
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
	// Create adds the login as an IRIS service account (no password) when
	// it is missing; Name and Email default to the login.
	Create bool   `json:"create,omitempty"`
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	// APIKeySecretRef, when set, is where the account's API key is kept for
	// the job that uses it: a stored key IRIS still accepts is left alone,
	// otherwise the key is renewed and stored.
	APIKeySecretRef *SecretRef `json:"apiKeySecretRef,omitempty"`
}

// Wazuh is one tenant's Wazuh manager API. The whole manager belongs
// to the tenant.
type Wazuh struct {
	// Tenant is the code of the tenant owning this manager.
	Tenant             string        `json:"tenant"`
	URL                string        `json:"url"`
	CredSecretRef      WazuhCredsRef `json:"credSecretRef"`
	CAFile             string        `json:"caFile,omitempty"`
	InsecureSkipVerify bool          `json:"insecureSkipVerify,omitempty"`
	// AdminAPIRole / AnalystAPIRole are the Wazuh API roles the
	// operator realm roles map to; TenantAPIRole is the one the
	// tenant's group maps to.
	AdminAPIRole   string `json:"adminApiRole,omitempty"`
	AnalystAPIRole string `json:"analystApiRole,omitempty"`
	TenantAPIRole  string `json:"tenantApiRole,omitempty"`
}

// WazuhCentral is the central OpenSearch indexer.
type WazuhCentral struct {
	URL                string        `json:"url"`
	CredSecretRef      WazuhCredsRef `json:"credSecretRef"`
	CAFile             string        `json:"caFile,omitempty"`
	InsecureSkipVerify bool          `json:"insecureSkipVerify,omitempty"`
	// Remotes are the cross-cluster search connections to ensure;
	// remotes not listed are never removed.
	Remotes []RemoteCluster `json:"remotes,omitempty"`
	// DashboardConfigSecret, when set, is the Secret the reconciler
	// writes the Wazuh dashboard app's wazuh.yml to, listing every
	// manager under components.wazuh with its API credentials.
	DashboardConfigSecret *ObjectRef `json:"dashboardConfigSecret,omitempty"`
	// DashboardURL is the central OpenSearch Dashboards (with the Wazuh
	// app) base URL, e.g. http://wazuh-dashboard:5601. The reconciler
	// signs in with CredSecretRef.
	DashboardURL string `json:"dashboardURL,omitempty"`
	// IndexPatterns are saved index patterns to ensure on the dashboard,
	// e.g. "*:wazuh-alerts-*" for cross-cluster search, which the Wazuh
	// app never creates because no local index matches it.
	IndexPatterns []IndexPattern `json:"indexPatterns,omitempty"`
}

// IndexPattern is one saved index pattern on the central dashboard.
type IndexPattern struct {
	Title string `json:"title"`
	// TimeFieldName defaults to DefaultIndexPatternTimeField.
	TimeFieldName string `json:"timeFieldName,omitempty"`
	// Default makes it the dashboard's defaultIndex when none is set or
	// the current one no longer exists. At most one pattern is default.
	Default bool `json:"default,omitempty"`
}

// DefaultIndexPatternTimeField is the Wazuh alerts time field.
const DefaultIndexPatternTimeField = "timestamp"

// ObjectRef names a namespaced object.
type ObjectRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// RemoteCluster is one cross-cluster search connection (sniff mode).
type RemoteCluster struct {
	Alias string   `json:"alias"`
	Seeds []string `json:"seeds"`
}

// EnrolmentBundle is a Secret holding what an agent needs to enrol
// with a tenant's manager: host, ports, the authd password (copied
// from AuthdSecretRef) and install scripts.
type EnrolmentBundle struct {
	Tenant           string    `json:"tenant"`
	Namespace        string    `json:"namespace"`
	Name             string    `json:"name"`
	ManagerHost      string    `json:"managerHost"`
	RegistrationPort int       `json:"registrationPort"`
	EventsPort       int       `json:"eventsPort"`
	AuthdSecretRef   SecretRef `json:"authdSecretRef"`
	// AgentVersion is the Wazuh agent package version the install
	// scripts fetch, e.g. "4.14.8-1". It must not be newer than the
	// manager.
	AgentVersion string `json:"agentVersion,omitempty"`
}

// WazuhCredsRef points at a Wazuh API or indexer user's Secret.
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
	for i := range c.Components.Wazuh {
		w := &c.Components.Wazuh[i]
		w.CredSecretRef.UsernameKey = orDefault(w.CredSecretRef.UsernameKey, DefaultWazuhUsernameKey)
		w.CredSecretRef.PasswordKey = orDefault(w.CredSecretRef.PasswordKey, DefaultWazuhPasswordKey)
		w.AdminAPIRole = orDefault(w.AdminAPIRole, DefaultWazuhAdminRole)
		w.AnalystAPIRole = orDefault(w.AnalystAPIRole, DefaultWazuhAnalystRole)
		w.TenantAPIRole = orDefault(w.TenantAPIRole, DefaultWazuhTenantRole)
	}
	for i := range c.Enrolment {
		e := &c.Enrolment[i]
		e.AgentVersion = orDefault(e.AgentVersion, DefaultWazuhAgentVersion)
	}
	if wc := c.Components.WazuhCentral; wc != nil {
		wc.CredSecretRef.UsernameKey = orDefault(wc.CredSecretRef.UsernameKey, DefaultIndexerUserKey)
		wc.CredSecretRef.PasswordKey = orDefault(wc.CredSecretRef.PasswordKey, DefaultIndexerPassKey)
		for i := range wc.IndexPatterns {
			p := &wc.IndexPatterns[i]
			p.TimeFieldName = orDefault(p.TimeFieldName, DefaultIndexPatternTimeField)
		}
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
	aliasPattern  = regexp.MustCompile(`^[a-z0-9_-]+$`)
	agentPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+-[0-9]+$`)
)

const maxPort = 65535

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
	c.validateSecretCopies(fail)
	c.validateComponents(fail)
	c.validateEnrolment(fail)
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
			switch {
			case k.Value != "":
			case k.Generator == GeneratorPassword, k.Generator == GeneratorHex32, k.Generator == GeneratorBase64:
			default:
				fail("%s.keys[%d].generator %q is not one of password, hex32, base64-32", where, j, k.Generator)
			}
			if k.Key == "" {
				fail("%s.keys[%d].key is required", where, j)
			}
		}
	}
}

func (c *Config) validateSecretCopies(fail func(string, ...any)) {
	targets := map[string]bool{}
	for i, cp := range c.SecretCopies {
		where := fmt.Sprintf("secretCopies[%d]", i)
		validateRef(fail, where+".from", cp.From)
		validateRef(fail, where+".to", cp.To)
		to := cp.To.Namespace + "/" + cp.To.Name + "/" + cp.To.Key
		switch {
		case cp.From == cp.To:
			fail("%s copies %s onto itself", where, to)
		case targets[to]:
			fail("%s.to %s is not unique", where, to)
		}
		targets[to] = true
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
			if sa.APIKeySecretRef != nil {
				validateRef(fail, fmt.Sprintf("components.iris.serviceAccounts[%d].apiKeySecretRef", i), *sa.APIKeySecretRef)
			}
		}
	}
	c.validateWazuh(fail)
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

func (c *Config) validateWazuh(fail func(string, ...any)) {
	tenants := c.tenantCodes()
	seen := map[string]bool{}
	for i, w := range c.Components.Wazuh {
		where := fmt.Sprintf("components.wazuh[%d]", i)
		switch {
		case !tenants[w.Tenant]:
			fail("%s.tenant %q is not a tenant code", where, w.Tenant)
		case seen[w.Tenant]:
			fail("%s.tenant %q already has a manager", where, w.Tenant)
		}
		seen[w.Tenant] = true
		if w.URL == "" {
			fail("%s.url is required", where)
		}
		if w.CredSecretRef.Namespace == "" || w.CredSecretRef.Name == "" {
			fail("%s.credSecretRef needs namespace and name", where)
		}
	}
	c.validateWazuhCentral(fail)
}

func (c *Config) validateWazuhCentral(fail func(string, ...any)) {
	wc := c.Components.WazuhCentral
	if wc == nil {
		return
	}
	if wc.URL == "" {
		fail("components.wazuhCentral.url is required")
	}
	if wc.CredSecretRef.Namespace == "" || wc.CredSecretRef.Name == "" {
		fail("components.wazuhCentral.credSecretRef needs namespace and name")
	}
	if d := wc.DashboardConfigSecret; d != nil && (d.Namespace == "" || d.Name == "") {
		fail("components.wazuhCentral.dashboardConfigSecret needs namespace and name")
	}
	validateIndexPatterns(fail, wc)
	aliases := map[string]bool{}
	for i, r := range wc.Remotes {
		where := fmt.Sprintf("components.wazuhCentral.remotes[%d]", i)
		switch {
		case !aliasPattern.MatchString(r.Alias):
			fail("%s.alias %q must match %s", where, r.Alias, aliasPattern)
		case aliases[r.Alias]:
			fail("%s.alias %q is not unique", where, r.Alias)
		}
		aliases[r.Alias] = true
		if len(r.Seeds) == 0 {
			fail("%s.seeds must not be empty", where)
		}
		for j, s := range r.Seeds {
			if strings.TrimSpace(s) == "" {
				fail("%s.seeds[%d] is empty", where, j)
			}
		}
	}
}

// validateIndexPatterns checks the central dashboard index patterns.
func validateIndexPatterns(fail func(string, ...any), wc *WazuhCentral) {
	if len(wc.IndexPatterns) > 0 && wc.DashboardURL == "" {
		fail("components.wazuhCentral.indexPatterns needs dashboardURL")
	}
	titles := map[string]bool{}
	defaults := 0
	for i, p := range wc.IndexPatterns {
		where := fmt.Sprintf("components.wazuhCentral.indexPatterns[%d]", i)
		switch {
		case strings.TrimSpace(p.Title) == "":
			fail("%s.title is required", where)
		case titles[p.Title]:
			fail("%s.title %q is not unique", where, p.Title)
		}
		titles[p.Title] = true
		if p.Default {
			defaults++
		}
	}
	if defaults > 1 {
		fail("components.wazuhCentral.indexPatterns: at most one pattern can be default, %d are", defaults)
	}
}

func (c *Config) validateEnrolment(fail func(string, ...any)) {
	tenants := c.tenantCodes()
	seen := map[string]bool{}
	for i, e := range c.Enrolment {
		where := fmt.Sprintf("enrolment[%d]", i)
		if !tenants[e.Tenant] {
			fail("%s.tenant %q is not a tenant code", where, e.Tenant)
		}
		if e.Namespace == "" || e.Name == "" {
			fail("%s needs namespace and name", where)
		} else if id := e.Namespace + "/" + e.Name; seen[id] {
			fail("%s: Secret %s is not unique", where, id)
		} else {
			seen[id] = true
		}
		if strings.TrimSpace(e.ManagerHost) == "" {
			fail("%s.managerHost is required", where)
		}
		if e.RegistrationPort < 1 || e.RegistrationPort > maxPort {
			fail("%s.registrationPort %d is not in 1-%d", where, e.RegistrationPort, maxPort)
		}
		if e.EventsPort < 1 || e.EventsPort > maxPort {
			fail("%s.eventsPort %d is not in 1-%d", where, e.EventsPort, maxPort)
		}
		validateRef(fail, where+".authdSecretRef", e.AuthdSecretRef)
		if !agentPattern.MatchString(e.AgentVersion) {
			fail("%s.agentVersion %q must look like 4.14.8-1", where, e.AgentVersion)
		}
	}
}

func (c *Config) tenantCodes() map[string]bool {
	out := make(map[string]bool, len(c.Tenants))
	for _, t := range c.Tenants {
		out[t.Code] = true
	}
	return out
}

func validateRef(fail func(string, ...any), where string, ref SecretRef) {
	if ref.Namespace == "" || ref.Name == "" || ref.Key == "" {
		fail("%s needs namespace, name and key", where)
	}
}
