// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

// Package login implements the `kubeaid-cli login` subcommand.
//
// login is intentionally self-contained: it does not parse general.yaml or
// secrets.yaml and does not proxy to the kubeaid-core container. It only
// reads three inputs — the puppet cert, the klist registry clone, and the
// kubeconfig output path — all of which can be overridden via flags, env
// vars, or their built-in defaults.
package login

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/klist"
)

const (
	flagCert       = "cert"
	flagRegistry   = "registry"
	flagKubeconfig = "kubeconfig"

	envCert       = "KUBEAID_CERT"
	envRegistry   = "KUBEAID_REGISTRY"
	envKubeconfig = "KUBECONFIG"

	defaultCert       = "~/.kubeaid/cert.pem"
	defaultRegistry   = "~/.config/klist"
	defaultKubeconfig = "~/.kube/config"
)

var flags struct {
	cert       string
	registry   string
	kubeconfig string
}

// LoginCmd is the cobra command for `kubeaid-cli login`.
var LoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Write a kubeconfig for the cluster identified by your puppet cert",
	Long: `login reads your puppet cert, derives the cluster and customer IDs
from the Subject CN, loads the matching YAML from your local klist clone,
and writes a kubeconfig that uses kubelogin for OIDC authentication.

No Docker, no general.yaml, no secrets.yaml needed.`,

	RunE: runLogin,
}

func init() {
	LoginCmd.Flags().StringVar(&flags.cert, flagCert, "",
		fmt.Sprintf("path to puppet cert PEM (env: %s, default: %s)", envCert, defaultCert))
	LoginCmd.Flags().StringVar(&flags.registry, flagRegistry, "",
		fmt.Sprintf("path to local klist clone (env: %s, default: %s)", envRegistry, defaultRegistry))
	LoginCmd.Flags().StringVar(&flags.kubeconfig, flagKubeconfig, "",
		fmt.Sprintf("kubeconfig output path (env: %s, default: %s)", envKubeconfig, defaultKubeconfig))
}

func runLogin(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	certPath := resolveInput(flags.cert, envCert, defaultCert)
	registryPath := resolveInput(flags.registry, envRegistry, defaultRegistry)
	kubeconfigPath := resolveInput(flags.kubeconfig, envKubeconfig, defaultKubeconfig)

	certPath = expandTilde(certPath)
	registryPath = expandTilde(registryPath)
	kubeconfigPath = expandTilde(kubeconfigPath)

	slog.InfoContext(ctx, "reading cert", slog.String("path", certPath))

	cn, err := cert.ReadCN(certPath)
	if err != nil {
		return fmt.Errorf("reading cert CN: %w", err)
	}

	clusterName, customerID, err := cert.SplitCN(cn)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "derived cluster identity",
		slog.String("clusterName", clusterName),
		slog.String("customerID", customerID),
	)

	cfg, err := klist.Load(registryPath, clusterName, customerID)
	if err != nil {
		return fmt.Errorf("loading cluster config from registry: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	kubeconfigBytes, err := buildKubeconfig(cfg, clusterName, customerID)
	if err != nil {
		return fmt.Errorf("building kubeconfig: %w", err)
	}

	if err := writeKubeconfig(kubeconfigPath, kubeconfigBytes); err != nil {
		return fmt.Errorf("writing kubeconfig: %w", err)
	}

	fmt.Printf("kubeconfig written to %s (cluster: %s.%s)\n",
		kubeconfigPath, clusterName, customerID)

	return nil
}

// resolveInput returns the first non-empty value among: explicit flag,
// environment variable, then the built-in default.
func resolveInput(flagVal, envKey, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}

	if v := os.Getenv(envKey); v != "" {
		return v
	}

	return defaultVal
}

// expandTilde replaces a leading "~/" with the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to literal path; the subsequent file operation will surface
		// the real error.
		return path
	}

	return filepath.Join(home, path[2:])
}

// kubeconfig is a minimal YAML representation of a kubeconfig. Using a local
// struct instead of k8s.io/client-go/tools/clientcmd/api avoids pulling in
// that package's json tag assumptions and allows us to emit exactly the shape
// defined in the design doc.
type kubeconfig struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Clusters       []namedCluster `yaml:"clusters"`
	Contexts       []namedContext `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
	Users          []namedUser    `yaml:"users"`
}

type namedCluster struct {
	Name    string      `yaml:"name"`
	Cluster clusterInfo `yaml:"cluster"`
}

type clusterInfo struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
}

type namedContext struct {
	Name    string      `yaml:"name"`
	Context contextInfo `yaml:"context"`
}

type contextInfo struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type namedUser struct {
	Name string   `yaml:"name"`
	User userInfo `yaml:"user"`
}

type userInfo struct {
	Exec execConfig `yaml:"exec"`
}

type execConfig struct {
	APIVersion string   `yaml:"apiVersion"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args"`
}

func buildKubeconfig(cfg *klist.ClusterConfig, clusterName, customerID string) ([]byte, error) {
	contextName := clusterName + "." + customerID

	caData := base64.StdEncoding.EncodeToString([]byte(cfg.CABundle))

	kc := kubeconfig{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []namedCluster{
			{
				Name: contextName,
				Cluster: clusterInfo{
					Server:                   cfg.Server,
					CertificateAuthorityData: caData,
				},
			},
		},
		Contexts: []namedContext{
			{
				Name: contextName,
				Context: contextInfo{
					Cluster: contextName,
					User:    "oidc",
				},
			},
		},
		CurrentContext: contextName,
		Users: []namedUser{
			{
				Name: "oidc",
				User: userInfo{
					Exec: execConfig{
						APIVersion: "client.authentication.k8s.io/v1beta1",
						Command:    "kubelogin",
						Args: []string{
							"get-token",
							"--oidc-issuer-url=" + cfg.OIDC.IssuerURL,
							"--oidc-client-id=" + cfg.OIDC.ClientID,
							"--oidc-extra-scope=email",
							"--oidc-extra-scope=groups",
						},
					},
				},
			},
		},
	}

	return yaml.Marshal(kc)
}

// writeKubeconfig creates intermediate directories and atomically writes
// content to path with mode 0600.
func writeKubeconfig(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating parent directories for %q: %w", path, err)
	}

	// Write to a temp file in the same directory first so that the final
	// rename is atomic on POSIX systems.
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".kubeconfig-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %q: %w", dir, err)
	}

	tmpName := tmp.Name()

	_, writeErr := tmp.Write(content)
	closeErr := tmp.Close()

	if writeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("writing kubeconfig temp file: %w", writeErr)
	}

	if closeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing kubeconfig temp file: %w", closeErr)
	}

	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("setting permissions on kubeconfig temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file to %q: %w", path, err)
	}

	return nil
}
