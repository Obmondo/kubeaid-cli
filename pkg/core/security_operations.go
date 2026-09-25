// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// securityOperationsCentralIndexerDN is the node DN of the central Wazuh
// indexer: CN=wazuh-indexer, the umbrella chart's certificates.subject
// organization, and the wazuh chart's default locality and country. Every
// tenant indexer admits it in indexer.config.nodesDn (cross-cluster search).
const securityOperationsCentralIndexerDN = "CN=wazuh-indexer,O=central,L=California,C=US"

// securityOperationsIRISMinLevel is the lowest Wazuh rule level forwarded to
// IRIS by the custom-iris integration (wazuh chart README section 10).
const securityOperationsIRISMinLevel = 10

// SecurityOperationsValues is what the security-operations templates render
// from: cluster.securityOperations with defaults applied, host names and
// ports derived, and the bcrypt hashes from secrets.yaml. Strings that may
// carry arbitrary text are pre-quoted (the *JSON fields) so the templates
// never have to escape.
type SecurityOperationsValues struct {
	// ChartRevision is the KubeAid revision the Applications use.
	ChartRevision string

	Domain     string
	HostPrefix string

	// Central host names.
	WazuhHost        string
	IRISHost         string
	MISPHost         string
	VelociraptorHost string

	KeycloakURL   string
	KeycloakRealm string
	// KeycloakIssuer is <url>/realms/<realm>.
	KeycloakIssuer string

	AgentHost    string
	AgentAddress string

	IngressClassName string
	ClusterIssuer    string

	ReconcilerEnabled          bool
	ReconcilerImageRepository  string
	ReconcilerImageTag         string
	ReconcilerImagePullSecrets []string
	ReconcilerDryRun           bool

	CentralIndexerDN string

	CentralIndexerPasswordHash   string
	CentralDashboardPasswordHash string

	// AdminRole and AnalystRole are the umbrella chart's operators roles.
	AdminRole   string
	AnalystRole string

	Tenants []SecurityOperationsTenantValues
}

// SecurityOperationsTenantValues is one tenant, fully derived.
type SecurityOperationsTenantValues struct {
	Code string
	// NameJSON is the display name as a JSON (and YAML double-quoted) string.
	NameJSON string
	// IRISOptions is the custom-iris <options> JSON. json.Marshal escapes
	// <, > and & as \u003c, \u003e and \u0026, so it is also safe XML text.
	IRISOptions string

	Namespace     string
	Group         string
	Organization  string
	DashboardHost string

	RetentionDays   int
	IndexerReplicas int

	RegistrationPort int
	EventsPort       int

	IndexerPasswordHash   string
	DashboardPasswordHash string
	ClusterKey            string
}

// securityOperationsSecret is one Secret to seal: the template and the data
// it renders from, and where the sealed file lands in the cluster directory.
type securityOperationsSecret struct {
	TemplateName string
	// RelativePath is sealed-secrets/<namespace>/<secret name>.yaml.
	RelativePath string
	Data         securityOperationsSecretData
}

// securityOperationsSecretData is what one Wazuh credentials Secret renders from.
type securityOperationsSecretData struct {
	Name      string
	Namespace string
	Password  string
}

// buildSecurityOperationsValues derives the template values of
// cluster.securityOperations. Nil when the SOC is not enabled.
func buildSecurityOperationsValues() *SecurityOperationsValues {
	if !config.SecurityOperationsEnabled() {
		return nil
	}
	cfg := config.ParsedGeneralConfig.Cluster.SecurityOperations
	creds := securityOperationsCredentials()

	host := func(name string) string {
		return fmt.Sprintf("%s%s.%s", cfg.HostPrefix, name, cfg.Domain)
	}

	chartRevision := cfg.ChartRevision
	if chartRevision == "" {
		chartRevision = config.ParsedGeneralConfig.Forks.KubeaidFork.Version
	}

	keycloakURL := strings.TrimSuffix(cfg.Keycloak.URL, "/")

	values := &SecurityOperationsValues{
		ChartRevision: chartRevision,

		Domain:     cfg.Domain,
		HostPrefix: cfg.HostPrefix,

		WazuhHost:        host("wazuh"),
		IRISHost:         host("iris"),
		MISPHost:         host("misp"),
		VelociraptorHost: host("velociraptor"),

		KeycloakURL:    keycloakURL,
		KeycloakRealm:  cfg.Keycloak.Realm,
		KeycloakIssuer: keycloakURL + "/realms/" + cfg.Keycloak.Realm,

		AgentHost:    cfg.AgentHost,
		AgentAddress: cfg.AgentAddress,

		IngressClassName: cfg.IngressClassName,
		ClusterIssuer:    cfg.ClusterIssuer,

		ReconcilerEnabled:          cfg.Reconciler.Enabled,
		ReconcilerImageRepository:  cfg.Reconciler.ImageRepository,
		ReconcilerImageTag:         cfg.Reconciler.ImageTag,
		ReconcilerImagePullSecrets: cfg.Reconciler.ImagePullSecrets,
		ReconcilerDryRun:           cfg.Reconciler.DryRun == nil || *cfg.Reconciler.DryRun,

		CentralIndexerDN: securityOperationsCentralIndexerDN,

		CentralIndexerPasswordHash:   creds.Central.IndexerPasswordHash,
		CentralDashboardPasswordHash: creds.Central.DashboardPasswordHash,

		AdminRole:   "administrator",
		AnalystRole: "analyst",
	}

	for _, tenant := range cfg.Tenants {
		tenantCreds := creds.Tenants[tenant.Code]
		group := constants.SecurityOperationsTenantGroupPrefix + tenant.Code

		values.Tenants = append(values.Tenants, SecurityOperationsTenantValues{
			Code:        tenant.Code,
			NameJSON:    mustJSON(tenant.Name),
			IRISOptions: irisOptions(tenant.Name),

			Namespace:     constants.SecurityOperationsTenantNamespacePrefix + tenant.Code,
			Group:         group,
			Organization:  group,
			DashboardHost: host(constants.SecurityOperationsTenantNamespacePrefix + tenant.Code),

			RetentionDays:   tenant.RetentionDays,
			IndexerReplicas: tenant.IndexerReplicas,

			RegistrationPort: tenant.AgentPorts.Registration,
			EventsPort:       tenant.AgentPorts.Events,

			IndexerPasswordHash:   tenantCreds.IndexerPasswordHash,
			DashboardPasswordHash: tenantCreds.DashboardPasswordHash,
			ClusterKey:            tenantCreds.ClusterKey,
		})
	}

	return values
}

// securityOperationsCredentials returns secrets.yaml's securityOperations
// block, never nil.
func securityOperationsCredentials() *config.SecurityOperationsCredentials {
	creds := config.ParsedSecretsConfig.SecurityOperations
	if creds == nil {
		creds = &config.SecurityOperationsCredentials{}
	}
	if creds.Tenants == nil {
		creds.Tenants = map[string]config.SecurityOperationsWazuhCredentials{}
	}
	return creds
}

// securityOperationsSecretFiles lists every Wazuh credentials Secret: the
// central indexer and dashboard logins in the security-operations namespace,
// and all four logins of each tenant in wazuh-<code>.
func securityOperationsSecretFiles() []securityOperationsSecret {
	if !config.SecurityOperationsEnabled() {
		return nil
	}
	creds := securityOperationsCredentials()

	secret := func(templateName, name, namespace, password string) securityOperationsSecret {
		return securityOperationsSecret{
			TemplateName: templateName,
			RelativePath: path.Join("sealed-secrets", namespace, name+".yaml"),
			Data: securityOperationsSecretData{
				Name:      name,
				Namespace: namespace,
				Password:  password,
			},
		}
	}

	central := constants.NamespaceSecurityOperations
	files := []securityOperationsSecret{
		secret(constants.TemplateNameWazuhIndexerCred,
			constants.SecretNameWazuhIndexerCred, central, creds.Central.IndexerPassword),
		secret(constants.TemplateNameWazuhDashboardCred,
			constants.SecretNameWazuhDashboardCred, central, creds.Central.DashboardPassword),
	}

	for _, tenant := range config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants {
		c := creds.Tenants[tenant.Code]
		ns := constants.SecurityOperationsTenantNamespacePrefix + tenant.Code
		files = append(files,
			secret(constants.TemplateNameWazuhIndexerCred,
				constants.SecretNameWazuhIndexerCred, ns, c.IndexerPassword),
			secret(constants.TemplateNameWazuhDashboardCred,
				constants.SecretNameWazuhDashboardCred, ns, c.DashboardPassword),
			secret(constants.TemplateNameWazuhAPICred,
				constants.SecretNameWazuhAPICred, ns, c.APIPassword),
			secret(constants.TemplateNameWazuhAuthdPass,
				constants.SecretNameWazuhAuthdPass, ns, c.AuthdPassword),
		)
	}

	return files
}

// createOrUpdateSecurityOperationsSealedSecretFiles seals every Wazuh
// credentials Secret into clusterDir, through SealIfPlaintextChanged so an
// unchanged Secret keeps its ciphertext. Returns the written paths, relative
// to clusterDir.
func createOrUpdateSecurityOperationsSealedSecretFiles(ctx context.Context, clusterDir string) ([]string, error) {
	var written []string

	for _, secret := range securityOperationsSecretFiles() {
		destinationFilePath := path.Join(clusterDir, secret.RelativePath)
		ctxWithPath := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("path", destinationFilePath),
		})

		if secret.Data.Password == "" {
			return written, fmt.Errorf("secrets.yaml has no password for %s/%s",
				secret.Data.Namespace, secret.Data.Name)
		}

		if err := utils.CreateIntermediateDirsForFile(destinationFilePath); err != nil {
			return written, fmt.Errorf("creating intermediate dirs for %s: %w", destinationFilePath, err)
		}

		plaintextBytes := templates.ParseAndExecuteTemplate(ctxWithPath,
			&KubeaidConfigFileTemplates,
			path.Join("templates/", secret.TemplateName),
			secret.Data,
		)

		if err := kubernetes.SealIfPlaintextChanged(ctxWithPath, destinationFilePath, plaintextBytes); err != nil {
			return written, fmt.Errorf("sealing %s: %w", destinationFilePath, err)
		}
		written = append(written, secret.RelativePath)
	}

	return written, nil
}

// mustJSON returns s as a JSON string literal, which is also a valid YAML
// double-quoted scalar.
func mustJSON(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string cannot fail.
		panic(err)
	}
	return string(out)
}

// irisOptions returns the custom-iris integration's <options> JSON for a
// tenant: every alert goes to that tenant's IRIS customer. json.Marshal's
// HTML escaping keeps it free of XML markup characters for ossec.conf.
func irisOptions(customerName string) string {
	options, err := json.Marshal(struct {
		MinLevel     int    `json:"min_level"`
		CustomerName string `json:"customer_name"`
	}{securityOperationsIRISMinLevel, customerName})
	if err != nil {
		panic(err)
	}
	return string(options)
}

// RenderSecurityOperations renders only the security operations files into
// clusterDir (a local k8s/<cluster> checkout): the Applications, both values
// files and the sealed Wazuh credentials. Nothing else in clusterDir is
// touched and no git operation runs. secrets.yaml must already be filled
// (parser.FillMissingSecrets). Returns the written paths, relative to
// clusterDir.
func RenderSecurityOperations(ctx context.Context, clusterDir string) ([]string, error) {
	if !config.SecurityOperationsEnabled() {
		return nil, fmt.Errorf("cluster.securityOperations is not enabled in general.yaml")
	}

	templateValues := &TemplateValues{
		ForksConfig: config.ParsedGeneralConfig.Forks,
		SecOps:      buildSecurityOperationsValues(),
	}

	written := make([]string, 0, len(constants.SecurityOperationsNonSecretTemplateNames))
	for _, templateName := range constants.SecurityOperationsNonSecretTemplateNames {
		relativePath := strings.TrimSuffix(templateName, ".tmpl")
		createFileFromTemplate(ctx, path.Join(clusterDir, relativePath), templateName, templateValues)
		written = append(written, relativePath)
	}

	sealed, err := createOrUpdateSecurityOperationsSealedSecretFiles(ctx, clusterDir)
	written = append(written, sealed...)
	return written, err
}
