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

	// SecurityOperationsDefaultReconcilerImage is the umbrella chart's
	// reconciler.image.repository.
	SecurityOperationsDefaultReconcilerImage = "ghcr.io/obmondo/siem-reconciler"

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
