// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"path"
	"slices"
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

	// KeycloakHost is the host of the Keycloak URL; KeycloakHostAliasIP, when
	// set, pins it in every pod (hostAliases).
	KeycloakHost        string
	KeycloakHostAliasIP string
	// ConfigRevision is the kubeaid-config revision of the values files.
	ConfigRevision string

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

	// VelociraptorEntryPoint, when set, renders the client TCP route.
	VelociraptorEntryPoint string
	// SharedStorageClass, when set, makes the IRIS volume ReadWriteMany.
	SharedStorageClass string
	ClusterIssuer      string

	// DashboardBreakGlass offers the username/password form next to SSO on
	// every Wazuh dashboard (wazuh chart dashboard.basicAuth.breakGlass).
	DashboardBreakGlass bool

	ReconcilerEnabled          bool
	ReconcilerImageRepository  string
	ReconcilerImageTag         string
	ReconcilerImagePullSecrets []string
	ReconcilerDryRun           bool

	// ContentEnabled switches the kubesoc-content package; ContentCanary is its
	// first tenant; ContentLists are the CDB lists every tenant registers.
	ContentEnabled bool
	ContentCanary  string
	ContentLists   []string

	AITriageEnabled bool
	AITriageDryRun  bool
	AIModel         string
	// AIDownloadModel opens Ollama's egress and pulls AIModel.
	AIDownloadModel bool
	// ReconcilerImage is the reconciler image repository with the chart's
	// default filled in: the Velociraptor API client publisher runs it too.
	ReconcilerImage string

	CentralIndexerDN string

	CentralIndexerPasswordHash   string
	CentralDashboardPasswordHash string

	// AdminRole and AnalystRole are the umbrella chart's operators roles.
	AdminRole   string
	AnalystRole string

	Tenants []SecurityOperationsTenantValues

	// SealedSecretsDir is SecurityOperationsSealedSecretsDir: each
	// Application's extra source is <it>/<its namespace>.
	SealedSecretsDir string

	// Sync is cluster.securityOperations.sync, resolved.
	SyncAutomated       bool
	SyncPrune           bool
	SyncServerSideApply bool
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
	// IndexerStorageSize is the tenant indexer's per-replica volume size
	// (securityOperationsIndexerStorageSize).
	IndexerStorageSize string

	RegistrationPort int
	EventsPort       int

	IndexerPasswordHash   string
	DashboardPasswordHash string
}

// securityOperationsSecret is one Secret to seal: the template and the data
// it renders from, and where the sealed file lands in the cluster directory.
type securityOperationsSecret struct {
	TemplateName string
	// RelativePath is security-operations/sealed-secrets/<namespace>/<secret
	// name>.yaml (securityOperationsSealedSecretPath): owned by the
	// Application of that namespace, not by the shared `secrets` app.
	RelativePath string
	Data         securityOperationsSecretData
	// SealOnce marks a Secret whose value no rendered file depends on (the
	// manager cluster key): kubeaid-cli siem render seals it when its file is
	// missing and otherwise leaves it alone, without regenerating the rest of
	// its namespace (credentialsFromClusterDir).
	SealOnce bool
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

	configRevision := cfg.ConfigRevision
	if configRevision == "" {
		configRevision = "HEAD"
	}

	chartRevision := cfg.ChartRevision
	if chartRevision == "" {
		chartRevision = config.ParsedGeneralConfig.Forks.KubeaidFork.Version
	}

	keycloakURL := strings.TrimSuffix(cfg.Keycloak.URL, "/")

	values := &SecurityOperationsValues{
		ChartRevision:  chartRevision,
		ConfigRevision: configRevision,

		Domain:     cfg.Domain,
		HostPrefix: cfg.HostPrefix,

		WazuhHost:        host("wazuh"),
		IRISHost:         host("iris"),
		MISPHost:         host("misp"),
		VelociraptorHost: host("velociraptor"),

		KeycloakURL:         keycloakURL,
		KeycloakHost:        keycloakHost(keycloakURL),
		KeycloakHostAliasIP: cfg.Keycloak.HostAliasIP,
		KeycloakRealm:       cfg.Keycloak.Realm,
		KeycloakIssuer:      keycloakURL + "/realms/" + cfg.Keycloak.Realm,

		AgentHost:    cfg.AgentHost,
		AgentAddress: cfg.AgentAddress,

		IngressClassName:       cfg.IngressClassName,
		VelociraptorEntryPoint: cfg.VelociraptorEntryPoint,
		SharedStorageClass:     cfg.SharedStorageClass,
		ClusterIssuer:          cfg.ClusterIssuer,
		DashboardBreakGlass:    cfg.DashboardBreakGlass,

		ReconcilerEnabled:          cfg.Reconciler.Enabled,
		ReconcilerImageRepository:  cfg.Reconciler.ImageRepository,
		ReconcilerImageTag:         cfg.Reconciler.ImageTag,
		ReconcilerImagePullSecrets: cfg.Reconciler.ImagePullSecrets,
		ReconcilerDryRun:           cfg.Reconciler.DryRun == nil || *cfg.Reconciler.DryRun,
		ReconcilerImage:            cfg.Reconciler.ImageRepository,

		ContentEnabled: cfg.Content.Enabled,
		ContentCanary:  cfg.Content.Canary,
		ContentLists:   append(append([]string(nil), constants.SecurityOperationsMISPLists...), cfg.Content.ExtraLists...),

		AITriageEnabled: cfg.AITriage.Enabled,
		AITriageDryRun:  cfg.AITriage.DryRun == nil || *cfg.AITriage.DryRun,
		AIModel:         cfg.AITriage.Model,
		AIDownloadModel: cfg.AITriage.DownloadModel,

		CentralIndexerDN: securityOperationsCentralIndexerDN,

		CentralIndexerPasswordHash:   creds.Central.IndexerPasswordHash,
		CentralDashboardPasswordHash: creds.Central.DashboardPasswordHash,

		AdminRole:   "administrator",
		AnalystRole: "analyst",

		SealedSecretsDir: SecurityOperationsSealedSecretsDir,

		SyncAutomated:       cfg.Sync.Automated,
		SyncPrune:           cfg.Sync.Automated && cfg.Sync.Prune,
		SyncServerSideApply: cfg.Sync.Automated,
	}
	if cfg.Sync.ServerSideApply != nil {
		values.SyncServerSideApply = *cfg.Sync.ServerSideApply
	}

	if values.AIModel == "" {
		values.AIModel = constants.SecurityOperationsDefaultAIModel
	}
	if values.ReconcilerImage == "" {
		values.ReconcilerImage = constants.SecurityOperationsDefaultReconcilerImage
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

			RetentionDays:      tenant.RetentionDays,
			IndexerReplicas:    tenant.IndexerReplicas,
			IndexerStorageSize: securityOperationsIndexerStorageSize(tenant),

			RegistrationPort: tenant.AgentPorts.Registration,
			EventsPort:       tenant.AgentPorts.Events,

			IndexerPasswordHash:   tenantCreds.IndexerPasswordHash,
			DashboardPasswordHash: tenantCreds.DashboardPasswordHash,
		})
	}

	return values
}

// securityOperationsIndexerStorageSize derives a tenant indexer's
// per-replica volume size: the explicit indexerStorageSize, or
// retentionDays (the chart default when unset) x expectedGBPerDay x
// headroom, rounded up to whole Gi and floored at the minimum. Each
// replica holds a full copy of the data (replica shards), so the size
// is per volume and replicas only multiply the total. A rendered
// StatefulSet volume never shrinks; growing an existing one needs a PVC
// patch plus a StatefulSet re-create (chart README, "Retention").
func securityOperationsIndexerStorageSize(tenant config.SecurityOperationsTenant) string {
	if tenant.IndexerStorageSize != "" {
		return tenant.IndexerStorageSize
	}
	days := tenant.RetentionDays
	if days == 0 {
		days = constants.SecurityOperationsDefaultRetentionDays
	}
	// The parser fills this in; default here too, so a config that did not
	// pass through it does not silently collapse to the minimum size.
	perDay := tenant.ExpectedGBPerDay
	if perDay == 0 {
		perDay = constants.SecurityOperationsDefaultGBPerDay
	}
	gi := int(math.Ceil(float64(days) * perDay * constants.SecurityOperationsIndexerHeadroom))
	if gi < constants.SecurityOperationsIndexerMinStorageGi {
		gi = constants.SecurityOperationsIndexerMinStorageGi
	}
	return fmt.Sprintf("%dGi", gi)
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
// and all four logins of each tenant in wazuh-<code>, plus its manager
// cluster key (SealOnce).
func securityOperationsSecretFiles() []securityOperationsSecret {
	if !config.SecurityOperationsEnabled() {
		return nil
	}
	creds := securityOperationsCredentials()

	secret := func(templateName, name, namespace, password string) securityOperationsSecret {
		return securityOperationsSecret{
			TemplateName: templateName,
			RelativePath: securityOperationsSealedSecretPath(namespace, name),
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
		clusterKey := secret(constants.TemplateNameWazuhClusterKey,
			constants.SecretNameWazuhClusterKey, ns, c.ClusterKey)
		clusterKey.SealOnce = true
		files = append(files, clusterKey)
	}

	return files
}

// createOrUpdateSecurityOperationsSealedSecretFiles seals every Wazuh
// credentials Secret and MISP's Valkey password into clusterDir, through
// SealIfPlaintextChanged so an unchanged Secret keeps its ciphertext. Returns
// the written paths, relative to clusterDir.
//
// Files listed in keep (paths relative to clusterDir) are left untouched (see
// credentialsFromClusterDir). The Valkey password is sealed only while its
// file is missing (mispRedisSecretFile).
func createOrUpdateSecurityOperationsSealedSecretFiles(ctx context.Context, clusterDir string,
	keep map[string]bool,
) ([]string, error) {
	var written []string

	files := securityOperationsSecretFiles()
	misp, err := mispRedisSecretFile(clusterDir)
	if err != nil {
		return written, err
	}
	if misp != nil {
		files = append(files, *misp)
	}

	for _, secret := range files {
		// A Secret still in the shared sealed-secrets/ directory is copied to
		// its owned location as it is (see adoptLegacySealedSecret).
		adopted, err := adoptLegacySealedSecret(clusterDir, secret.Data.Namespace, secret.Data.Name)
		if err != nil {
			return written, fmt.Errorf("moving %s/%s to %s: %w", secret.Data.Namespace, secret.Data.Name,
				SecurityOperationsSealedSecretsDir, err)
		}
		written = append(written, adopted...)

		// A kept release keeps the files it already has. A Secret sealed once
		// (the manager cluster key) can still be missing from a kept release
		// rendered before it existed, and is sealed now.
		if keep[secret.Data.Namespace] && fileExists(path.Join(clusterDir, secret.RelativePath)) {
			if err := annotateSealedSecretFile(path.Join(clusterDir, secret.RelativePath)); err != nil {
				return written, err
			}
			continue
		}
		destinationFilePath := path.Join(clusterDir, secret.RelativePath)
		ctxWithPath := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("path", destinationFilePath),
		})

		if secret.Data.Password == "" {
			return written, fmt.Errorf("no password for %s/%s",
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
		if err := annotateSealedSecretFile(destinationFilePath); err != nil {
			return written, err
		}
		// Until the legacy copy is removed, both the `secrets` app and the
		// owning SOC app apply this SealedSecret: they must apply the same one.
		legacy, err := syncLegacySealedSecret(clusterDir, secret.Data.Namespace, secret.Data.Name)
		if err != nil {
			return written, err
		}
		written = append(written, legacy...)
		if !slices.Contains(written, secret.RelativePath) {
			written = append(written, secret.RelativePath)
		}
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
// touched and no git operation runs. It needs no secrets.yaml: the Wazuh
// credentials come from clusterDir itself (credentialsFromClusterDir), so a
// release rendered before keeps its passwords and new ones get fresh ones; a
// release whose state is incomplete is an error, never a silent rotation.
// Returns the written paths, relative to clusterDir.
func RenderSecurityOperations(ctx context.Context, clusterDir string) ([]string, error) {
	return RenderSecurityOperationsWithOptions(ctx, clusterDir, SecurityOperationsRenderOptions{})
}

// RenderSecurityOperationsWithOptions is RenderSecurityOperations with
// deliberate credential rotation (opts.Rotate).
func RenderSecurityOperationsWithOptions(ctx context.Context, clusterDir string,
	opts SecurityOperationsRenderOptions,
) ([]string, error) {
	if !config.SecurityOperationsEnabled() {
		return nil, fmt.Errorf("cluster.securityOperations is not enabled in general.yaml")
	}

	rotate, err := opts.rotateSet()
	if err != nil {
		return nil, err
	}
	creds, keep, err := credentialsFromClusterDir(clusterDir, rotate)
	if err != nil {
		return nil, fmt.Errorf("reading the credential state in %s: %w", clusterDir, err)
	}
	config.ParsedSecretsConfig.SecurityOperations = creds

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

	sealed, err := createOrUpdateSecurityOperationsSealedSecretFiles(ctx, clusterDir, keep)
	written = append(written, sealed...)
	return written, err
}

// keycloakHost returns the host name of a Keycloak URL, "" when it has none.
func keycloakHost(keycloakURL string) string {
	u, err := url.Parse(keycloakURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
