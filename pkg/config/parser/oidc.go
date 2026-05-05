// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// hydrateWithOIDCOptions translates the typed cluster.apiServer.oidc
// block in general.yaml into a structured AuthenticationConfiguration
// YAML body delivered via apiServer.Files (CAPI's KubeadmControlPlane
// writes the file via cloud-init), plus the matching
// `--authentication-config` flag and a host-path mount.
//
// When --authentication-config is set, kube-apiserver ignores the
// legacy --oidc-* flags entirely, so any stale ExtraArgs from
// previous configurations are inert (no need to scrub them).
func hydrateWithOIDCOptions() {
	cfg := config.ParsedGeneralConfig.Cluster.APIServer.OIDC
	if cfg == nil {
		return
	}

	body, err := renderAuthenticationConfig(cfg)
	if err != nil {
		// CABundlePath is the only failure path (file unreadable);
		// re-surface with the field name so the user can fix it.
		panic(fmt.Errorf("rendering kube-apiserver AuthenticationConfiguration: %w", err))
	}

	upsertAPIServerFile(constants.KubeAPIServerAuthenticationConfigPath, body)

	args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
	if args == nil {
		args = map[string]string{}
		config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs = args
	}
	args[constants.KubeAPIServerFlagAuthenticationConfig] = constants.KubeAPIServerAuthenticationConfigPath

	ensureHostPathGetsMounted(config.HostPathMountConfig{
		Name:      constants.KubeAPIServerFlagAuthenticationConfig,
		HostPath:  constants.KubeAPIServerAuthenticationConfigPath,
		MountPath: constants.KubeAPIServerAuthenticationConfigPath,
		ReadOnly:  true,
		PathType:  v1.HostPathFile,
	})
}

// authenticationConfig mirrors k8s.io/apiserver's AuthenticationConfiguration
// shape (apiserver.config.k8s.io/v1) restricted to the fields kubeaid-cli
// emits. Inlining the type avoids a dependency on
// k8s.io/apiserver/pkg/apis/apiserver and keeps the YAML deterministic.
type authenticationConfig struct {
	APIVersion string                    `json:"apiVersion"`
	Kind       string                    `json:"kind"`
	JWT        []authenticationConfigJWT `json:"jwt"`
}

type authenticationConfigJWT struct {
	Issuer        authenticationConfigIssuer        `json:"issuer"`
	ClaimMappings authenticationConfigPrefixedClaims `json:"claimMappings"`
}

type authenticationConfigIssuer struct {
	URL                  string   `json:"url"`
	Audiences            []string `json:"audiences"`
	CertificateAuthority string   `json:"certificateAuthority,omitempty"`
}

type authenticationConfigPrefixedClaims struct {
	Username authenticationConfigPrefixedClaim `json:"username"`
	Groups   authenticationConfigPrefixedClaim `json:"groups"`
}

type authenticationConfigPrefixedClaim struct {
	Claim  string `json:"claim"`
	Prefix string `json:"prefix"`
}

// renderAuthenticationConfig builds the structured YAML body for one
// JWT issuer. Multi-issuer support (Obmondo SRE access) is captured
// in the design doc as future work.
func renderAuthenticationConfig(cfg *config.OIDCConfig) (string, error) {
	doc := authenticationConfig{
		APIVersion: "apiserver.config.k8s.io/v1",
		Kind:       "AuthenticationConfiguration",
		JWT: []authenticationConfigJWT{{
			Issuer: authenticationConfigIssuer{
				URL:       cfg.IssuerURL,
				Audiences: []string{cfg.ClientID},
			},
			ClaimMappings: authenticationConfigPrefixedClaims{
				Username: authenticationConfigPrefixedClaim{
					Claim:  cfg.UsernameClaim,
					Prefix: cfg.UsernamePrefix,
				},
				Groups: authenticationConfigPrefixedClaim{
					Claim:  cfg.GroupsClaim,
					Prefix: cfg.GroupsPrefix,
				},
			},
		}},
	}

	if cfg.CABundlePath != "" {
		ca, err := os.ReadFile(cfg.CABundlePath)
		if err != nil {
			return "", fmt.Errorf(
				"reading apiServer.oidc.caBundlePath %q: %w",
				cfg.CABundlePath, err,
			)
		}
		doc.JWT[0].Issuer.CertificateAuthority = string(ca)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("marshalling AuthenticationConfiguration: %w", err)
	}
	return string(out), nil
}

// upsertAPIServerFile writes content to the apiServer.Files entry
// for path; if the user already supplied a file at the same path,
// kubeaid-cli replaces the content (typed config is authoritative).
func upsertAPIServerFile(path, content string) {
	files := config.ParsedGeneralConfig.Cluster.APIServer.Files
	for i := range files {
		if files[i].Path == path {
			files[i].Content = content
			return
		}
	}
	config.ParsedGeneralConfig.Cluster.APIServer.Files = append(files, config.FileConfig{
		Path:    path,
		Content: content,
	})
}
