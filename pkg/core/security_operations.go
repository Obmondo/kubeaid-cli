// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
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

// Wazuh manager roles the per-tenant agent Service may front.
const (
	securityOperationsNodeTypeMaster = "master"
	securityOperationsNodeTypeWorker = "worker"
	// schemeHTTP is the only object store scheme without TLS.
	schemeHTTP = "http"
)

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
	// set, pins it in every pod (hostAliases, deprecated).
	KeycloakHost        string
	KeycloakHostAliasIP string
	// KeycloakInternalURL, when set, is the URL every back-channel call uses
	// while KeycloakURL stays the issuer.
	KeycloakInternalURL string
	// KeycloakDiscoveryURL is the OIDC discovery endpoint as the pods fetch
	// it: the internal URL where there is one, else the public one.
	KeycloakDiscoveryURL string
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

	AITriageEnabled bool
	AITriageDryRun  bool
	AIModel         string
	// AIDownloadModel opens Ollama's egress and pulls AIModel.
	AIDownloadModel bool
	// ReconcilerImage is the reconciler image repository with the chart's
	// default filled in: the Velociraptor API client publisher runs it too.
	ReconcilerImage string

	CentralIndexerDN string

	// Profile is the resolved deployment profile; Backup the resolved backup
	// block. Both are never nil.
	Profile *SecurityOperationsProfileValues
	Backup  *SecurityOperationsBackupValues

	CentralIndexerPasswordHash   string
	CentralDashboardPasswordHash string

	// AdminRole and AnalystRole are the umbrella chart's operators roles.
	AdminRole   string
	AnalystRole string

	Tenants []SecurityOperationsTenantValues
}

// SecurityOperationsProfileValues is cluster.securityOperations.profile
// resolved into the numbers the templates render. Standard, the default, is
// what every release rendered before profiles existed: HA and Single are
// false, so the templates emit nothing and the charts' own defaults stand.
type SecurityOperationsProfileValues struct {
	Name        string
	TopologyKey string

	// Single is the one-of-everything profile: no PodDisruptionBudget, so a
	// single-node cluster can still be drained, and no required anti-affinity.
	Single bool
	// HA renders the resilient shape.
	HA bool

	// IndexerNodes is what a tenant that named no indexerReplicas gets, and
	// IndexerMinAvailable the budget that goes with it.
	IndexerNodes        int
	IndexerMinAvailable int
	// IndexShardReplicas is the OpenSearch number_of_replicas of the alert
	// indices; a copy needs another node to live on.
	IndexShardReplicas int

	// ManagerWorkers behind the agent Service. 0 keeps the master-only shape,
	// where the agent Service selects the master itself.
	ManagerWorkers int
	// AgentServiceNodeType is the manager role the agent Service fronts.
	AgentServiceNodeType string

	// Replicas of the components that scale horizontally.
	DashboardReplicas  int
	IRISAppReplicas    int
	IRISWorkerReplicas int
	// DatabaseInstances is the CloudNativePG, MariaDB and RabbitMQ instance count.
	DatabaseInstances int
}

// SecurityOperationsBackupValues is cluster.securityOperations.backup with the
// placeholders resolved. Enabled false renders nothing.
type SecurityOperationsBackupValues struct {
	Enabled           bool
	Bucket            string
	Endpoint          string
	Region            string
	BasePath          string
	CredentialsSecret string
	PathStyleAccess   bool
	VeleroNamespace   string
	RetentionDays     int
	VerifyRestore     bool

	// SnapshotRepositoryType is "fs" or "s3"; SnapshotFS is true for "fs",
	// which also needs the shared volume on every Wazuh release.
	SnapshotRepositoryType string
	SnapshotFS             bool
	SnapshotVolumePath     string
	SnapshotVolumeSize     string
	SnapshotStorageClass   string

	// MariaDBEndpoint is the object store as mariadb-operator wants it: host
	// and port without a scheme. MariaDBTLS is false for a plain-HTTP one.
	MariaDBEndpoint string
	MariaDBTLS      bool
	// MariaDBMaxRetention is RetentionDays as the Go duration mariadb-operator
	// takes (it has no day unit).
	MariaDBMaxRetention string

	// PostgresDestination is the barman destinationPath of the IRIS database.
	PostgresDestination string
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
}

// securityOperationsSecret is one Secret to seal: the template and the data
// it renders from, and where the sealed file lands in the cluster directory.
type securityOperationsSecret struct {
	TemplateName string
	// RelativePath is sealed-secrets/<namespace>/<secret name>.yaml.
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

	// The back channel, where there is one. The issuer stays public: Keycloak
	// puts its own frontend URL in the discovery document whatever address it
	// was fetched from, so the tokens still verify.
	keycloakInternalURL := strings.TrimSuffix(cfg.Keycloak.InternalURL, "/")
	discoveryBase := keycloakURL
	if keycloakInternalURL != "" {
		discoveryBase = keycloakInternalURL
	}
	discoveryURL := discoveryBase + "/realms/" + cfg.Keycloak.Realm +
		"/.well-known/openid-configuration"

	values := &SecurityOperationsValues{
		ChartRevision:  chartRevision,
		ConfigRevision: configRevision,

		Domain:     cfg.Domain,
		HostPrefix: cfg.HostPrefix,

		WazuhHost:        host("wazuh"),
		IRISHost:         host("iris"),
		MISPHost:         host("misp"),
		VelociraptorHost: host("velociraptor"),

		KeycloakURL:  keycloakURL,
		KeycloakHost: keycloakHost(keycloakURL),
		//nolint:staticcheck // the deprecated fallback is still rendered on purpose
		KeycloakHostAliasIP:  cfg.Keycloak.HostAliasIP,
		KeycloakInternalURL:  keycloakInternalURL,
		KeycloakDiscoveryURL: discoveryURL,
		KeycloakRealm:        cfg.Keycloak.Realm,
		KeycloakIssuer:       keycloakURL + "/realms/" + cfg.Keycloak.Realm,

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

		AITriageEnabled: cfg.AITriage.Enabled,
		AITriageDryRun:  cfg.AITriage.DryRun == nil || *cfg.AITriage.DryRun,
		AIModel:         cfg.AITriage.Model,
		AIDownloadModel: cfg.AITriage.DownloadModel,

		CentralIndexerDN: securityOperationsCentralIndexerDN,

		Profile: securityOperationsProfile(cfg),
		Backup:  securityOperationsBackup(cfg),

		CentralIndexerPasswordHash:   creds.Central.IndexerPasswordHash,
		CentralDashboardPasswordHash: creds.Central.DashboardPasswordHash,

		AdminRole:   "administrator",
		AnalystRole: "analyst",
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

			RetentionDays:   tenant.RetentionDays,
			IndexerReplicas: tenant.IndexerReplicas,

			RegistrationPort: tenant.AgentPorts.Registration,
			EventsPort:       tenant.AgentPorts.Events,

			IndexerPasswordHash:   tenantCreds.IndexerPasswordHash,
			DashboardPasswordHash: tenantCreds.DashboardPasswordHash,
		})
	}

	return values
}

// securityOperationsProfile resolves cluster.securityOperations.profile. The
// standard profile is deliberately empty: a cluster that renders today must
// keep rendering the same, so only single and ha depart from the charts' own
// defaults.
func securityOperationsProfile(cfg *config.SecurityOperationsConfig) *SecurityOperationsProfileValues {
	profile := &SecurityOperationsProfileValues{
		Name:                 cfg.Profile,
		TopologyKey:          cfg.TopologyKey,
		IndexerNodes:         1,
		IndexShardReplicas:   0,
		AgentServiceNodeType: securityOperationsNodeTypeMaster,
		DashboardReplicas:    1,
		IRISAppReplicas:      1,
		IRISWorkerReplicas:   1,
		DatabaseInstances:    1,
	}

	switch cfg.Profile {
	case constants.SecurityOperationsProfileSingle:
		profile.Single = true

	case constants.SecurityOperationsProfileHA:
		profile.HA = true
		profile.IndexerNodes = constants.SecurityOperationsHAIndexerNodes
		profile.IndexerMinAvailable = constants.SecurityOperationsHAIndexerMinAvailable
		profile.IndexShardReplicas = constants.SecurityOperationsHAIndexShardReplicas
		// One worker takes the agents' events while the master keeps
		// enrolment and the API, so a master restart is not an outage for
		// the agents. The agent Service then fronts the workers, which
		// forward enrolment to the master.
		profile.ManagerWorkers = 2
		profile.AgentServiceNodeType = securityOperationsNodeTypeWorker
		profile.DashboardReplicas = 2
		profile.IRISAppReplicas = 2
		profile.IRISWorkerReplicas = 2
		profile.DatabaseInstances = constants.SecurityOperationsHADatabaseInstances
	}

	return profile
}

// securityOperationsBackup resolves cluster.securityOperations.backup into the
// per-backend shapes the four charts want.
func securityOperationsBackup(cfg *config.SecurityOperationsConfig) *SecurityOperationsBackupValues {
	backup := cfg.Backup

	values := &SecurityOperationsBackupValues{
		Enabled:           backup.Enabled,
		Bucket:            backup.Bucket,
		Endpoint:          backup.Endpoint,
		Region:            backup.Region,
		BasePath:          backup.BasePath,
		CredentialsSecret: backup.CredentialsSecret,
		PathStyleAccess:   backup.PathStyleAccess == nil || *backup.PathStyleAccess,
		VeleroNamespace:   backup.VeleroNamespace,
		RetentionDays:     backup.RetentionDays,
		VerifyRestore:     backup.VerifyRestore,

		SnapshotRepositoryType: backup.SnapshotRepositoryType,
		SnapshotFS:             backup.SnapshotRepositoryType != constants.SecurityOperationsSnapshotRepositoryS3,
		SnapshotVolumePath:     constants.SecurityOperationsSnapshotVolumePath,
		SnapshotVolumeSize:     backup.SnapshotVolumeSize,
		SnapshotStorageClass:   backup.SnapshotStorageClass,

		// mariadb-operator takes a host and port, not a URL, and switches TLS
		// with a flag of its own.
		MariaDBEndpoint:     objectStoreHostPort(backup.Endpoint),
		MariaDBTLS:          !strings.HasPrefix(backup.Endpoint, schemeHTTP+"://"),
		MariaDBMaxRetention: fmt.Sprintf("%dh", backup.RetentionDays*hoursPerDay),

		PostgresDestination: fmt.Sprintf("s3://%s/%s/iris-pgsql", backup.Bucket, backup.BasePath),
	}

	return values
}

// objectStoreHostPort turns an object store URL into the host[:port] form
// mariadb-operator wants, filling in the default port of the scheme. An empty
// endpoint means AWS S3.
func objectStoreHostPort(endpoint string) string {
	if endpoint == "" {
		return "s3.amazonaws.com"
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == schemeHTTP {
		return u.Hostname() + ":80"
	}
	return u.Hostname() + ":443"
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
		if keep[secret.RelativePath] {
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
// touched and no git operation runs. It needs no secrets.yaml: the Wazuh
// credentials come from clusterDir itself (credentialsFromClusterDir), so a
// release rendered before keeps its passwords and new ones get fresh ones.
// Returns the written paths, relative to clusterDir.
func RenderSecurityOperations(ctx context.Context, clusterDir string) ([]string, error) {
	if !config.SecurityOperationsEnabled() {
		return nil, fmt.Errorf("cluster.securityOperations is not enabled in general.yaml")
	}

	creds, keep, err := credentialsFromClusterDir(clusterDir)
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
