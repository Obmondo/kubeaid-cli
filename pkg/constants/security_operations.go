// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package constants

// ClusterIssuerLetsEncrypt is the cert-manager ClusterIssuer kubeaid-cli renders
// (values-cert-manager.yaml) and the Ingress annotations reference.
const ClusterIssuerLetsEncrypt = "letsencrypt-prod"

// Security operations (cluster.securityOperations): the KubeAid
// security-operations umbrella chart and one wazuh release per tenant.
const (
	ArgoCDAppSecurityOperations = "security-operations"
	NamespaceSecurityOperations = "security-operations"

	// A tenant's Wazuh: Application and namespace <prefix><code>, Keycloak
	// group and realm role <group prefix><code> (umbrella chart defaults).
	SecurityOperationsTenantNamespacePrefix = "wazuh-"
	SecurityOperationsTenantGroupPrefix     = "tenant-"

	SecurityOperationsDefaultRealm         = "soc"
	SecurityOperationsDefaultIngressClass  = "traefik"
	SecurityOperationsDefaultAgentPortBase = 20000

	// Deployment profiles (cluster.securityOperations.profile). Standard is
	// what every release rendered before profiles existed, so it adds
	// nothing; single and ha are the two deliberate departures from it.
	SecurityOperationsProfileSingle   = "single"
	SecurityOperationsProfileStandard = "standard"
	SecurityOperationsProfileHA       = "ha"

	// SecurityOperationsDefaultTopologyKey is the failure domain a profile
	// spreads replicas over.
	SecurityOperationsDefaultTopologyKey = "kubernetes.io/hostname"

	// Indexer nodes per tenant in each profile, and the shard copies of the
	// alert indices that go with them (a copy needs a node to live on).
	SecurityOperationsHAIndexerNodes        = 3
	SecurityOperationsHAIndexerMinAvailable = 2
	SecurityOperationsHAIndexShardReplicas  = 1

	// Instances of the databases and the broker in the ha profile.
	SecurityOperationsHADatabaseInstances = 3

	// SecurityOperationsBackupDefault* are the object store placeholders: we
	// have no bucket yet, so they only have to be obviously fillable.
	SecurityOperationsBackupDefaultRegion            = "us-east-1"
	SecurityOperationsBackupDefaultBasePath          = "kubesoc"
	SecurityOperationsBackupDefaultCredentialsSecret = "kubesoc-backup-s3"
	SecurityOperationsBackupDefaultVeleroNamespace   = "velero"
	SecurityOperationsBackupDefaultRetentionDays     = 30
	SecurityOperationsBackupDefaultSnapshotSize      = "50Gi"

	// SecurityOperationsSnapshotRepository* are the OpenSearch snapshot
	// repository types. The stock wazuh-indexer image ships no repository-s3
	// plugin, so fs (a ReadWriteMany volume) is the default.
	SecurityOperationsSnapshotRepositoryFS = "fs"
	SecurityOperationsSnapshotRepositoryS3 = "s3"

	// SecurityOperationsSnapshotVolumePath is where every indexer node mounts
	// the shared snapshot volume (the wazuh chart's indexer.snapshot.fs.path,
	// which is also its path.repo).
	SecurityOperationsSnapshotVolumePath = "/mnt/snapshots"

	// SecurityOperationsDefaultReconcilerImage is the umbrella chart's
	// reconciler.image.repository.
	SecurityOperationsDefaultReconcilerImage = "ghcr.io/obmondo/siem-reconciler"

	// SecurityOperationsDefaultAIModel is the dfir-iris aiTriage default model.
	SecurityOperationsDefaultAIModel = "llama3.1:8b"

	// Secrets every Wazuh release reads (wazuh chart README section 11).
	SecretNameWazuhIndexerCred   = "wazuh-indexer-cred"
	SecretNameWazuhDashboardCred = "wazuh-dashboard-cred"
	SecretNameWazuhAPICred       = "wazuh-api-cred"
	SecretNameWazuhAuthdPass     = "wazuh-authd-pass"
)

// SecurityOperationsNonSecretTemplateNames are the Applications (central plus
// one per tenant, in one file) and the two values files.
var SecurityOperationsNonSecretTemplateNames = []string{
	"argocd-apps/templates/security-operations.yaml.tmpl",
	"argocd-apps/values-security-operations.yaml.tmpl",
	"argocd-apps/values-wazuh-tenant.yaml.tmpl",
}

// Secret templates of the Wazuh credentials, rendered once per namespace into
// sealed-secrets/<namespace>/<secret name>.yaml (see core.securityOperationsSecretFiles).
const (
	TemplateNameWazuhIndexerCred   = "sealed-secrets/security-operations/wazuh-indexer-cred.yaml.tmpl"
	TemplateNameWazuhDashboardCred = "sealed-secrets/security-operations/wazuh-dashboard-cred.yaml.tmpl"
	TemplateNameWazuhAPICred       = "sealed-secrets/security-operations/wazuh-api-cred.yaml.tmpl"
	TemplateNameWazuhAuthdPass     = "sealed-secrets/security-operations/wazuh-authd-pass.yaml.tmpl"
)

// Secrets sealed once and then left as they are while their sealed file exists
// (core.createOrUpdateSecurityOperationsSealedSecretFiles): the manager cluster
// key of each tenant (wazuh chart wazuh.clusterKeySecret) and MISP's Valkey
// password (misp chart env.redisPasswordSecret, valkey usersExistingSecret).
const (
	SecretNameWazuhClusterKey = "wazuh-manager-cluster-key"
	SecretNameMISPRedis       = "misp-redis"

	TemplateNameWazuhClusterKey = "sealed-secrets/security-operations/wazuh-manager-cluster-key.yaml.tmpl"
	TemplateNameMISPRedis       = "sealed-secrets/security-operations/misp-redis.yaml.tmpl"
)
