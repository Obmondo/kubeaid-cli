// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	v1 "k8s.io/api/core/v1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// oidcCAFileMountPath is the in-pod location at which a user-supplied
// OIDC issuer CA bundle is mounted into the kube-apiserver container.
// The host path comes from OIDCConfig.CABundlePath; the mount path is
// fixed so the --oidc-ca-file flag can reference a stable location.
const oidcCAFileMountPath = "/etc/kubernetes/pki/oidc-ca.pem"

// hydrateWithOIDCOptions translates the typed cluster.apiServer.oidc
// block in general.yaml into the corresponding kube-apiserver
// `--oidc-*` flags on APIServer.ExtraArgs and (when CABundlePath is
// set) ensures the CA file is mounted into the apiserver pod.
//
// The typed block is authoritative: if the same flag also appears in
// ExtraArgs, the typed value wins. Users who don't want the typed
// translation can simply leave the oidc block unset and configure
// --oidc-* flags via ExtraArgs directly.
func hydrateWithOIDCOptions() {
	cfg := config.ParsedGeneralConfig.Cluster.APIServer.OIDC
	if cfg == nil {
		return
	}

	args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
	if args == nil {
		args = map[string]string{}
		config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs = args
	}

	args[constants.KubeAPIServerFlagOIDCIssuerURL] = cfg.IssuerURL
	args[constants.KubeAPIServerFlagOIDCClientID] = cfg.ClientID
	args[constants.KubeAPIServerFlagOIDCUsernameClaim] = cfg.UsernameClaim
	args[constants.KubeAPIServerFlagOIDCGroupsClaim] = cfg.GroupsClaim

	if cfg.UsernamePrefix != "" {
		args[constants.KubeAPIServerFlagOIDCUsernamePrefix] = cfg.UsernamePrefix
	}

	if cfg.GroupsPrefix != "" {
		args[constants.KubeAPIServerFlagOIDCGroupsPrefix] = cfg.GroupsPrefix
	}

	// CABundlePath is optional; only deployments where Keycloak's TLS
	// is signed by a private CA need it. When set, mount the host
	// file into the apiserver pod and point --oidc-ca-file at the
	// fixed in-pod path. Reuses ensureHostPathGetsMounted (defined in
	// audit_logging.go) so we don't duplicate already-mounted volumes.
	if cfg.CABundlePath != "" {
		args[constants.KubeAPIServerFlagOIDCCAFile] = oidcCAFileMountPath

		ensureHostPathGetsMounted(config.HostPathMountConfig{
			Name:      constants.KubeAPIServerFlagOIDCCAFile,
			HostPath:  cfg.CABundlePath,
			MountPath: oidcCAFileMountPath,
			ReadOnly:  true,
			PathType:  v1.HostPathFile,
		})
	}
}
