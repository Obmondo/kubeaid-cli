// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
)

var bundleFlags struct {
	kubeaidDir string
	out        string
	version    string
	content    string
	models     string
	platform   string
	skipImages bool
	extra      []string

	bundle    string
	registry  string
	insecure  bool
	extractTo string
}

// BundleCmd is `siem bundle`.
var BundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Build an air-gapped bundle: images, charts, content and models",
	Long: `Renders every security operations release (helm template of the charts in
--kubeaid-dir with the values rendered into --cluster-dir; run 'siem render'
first), collects every image they reference, pulls them (linux/amd64 by default,
through ~/.docker/config.json credentials) into an OCI layout, and writes one tar:

  manifest.json   version, chart revision, images with digests, releases
  images/         OCI image layout (shared layers stored once)
  charts/         the charts, symlinked sub-charts resolved
  content/        --content (rules, decoders, dashboards), when given
  models/         --ollama-models (an Ollama models directory), when given

On the air-gapped side, 'siem bundle import' pushes the images to a registry.
Without go-containerregistry, the same can be done with skopeo:
'skopeo copy docker://<image> oci:images:<image>' per manifest.json entry.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		releases, err := socReleases(tenantCodes(cfg))
		if err != nil {
			return err
		}
		version := bundleFlags.version
		if version == "" {
			version = cfg.ChartRevision
		}
		if version == "" {
			version = "dev"
		}
		out := bundleFlags.out
		if out == "" {
			out = "kubesoc-" + strings.ReplaceAll(version, "/", "-") + ".tar"
		}
		m, err := siemctl.BuildBundle(cmd.Context(), siemctl.BundleOptions{
			Out:           out,
			Version:       version,
			ChartRevision: cfg.ChartRevision,
			Releases:      releases,
			KubeaidDir:    bundleFlags.kubeaidDir,
			ContentDir:    bundleFlags.content,
			ModelsDir:     bundleFlags.models,
			Platform:      bundleFlags.platform,
			SkipImages:    bundleFlags.skipImages,
			ExtraImages:   bundleFlags.extra,
			Log:           cmd.ErrOrStderr(),
		})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s: %d images, %d charts, %d models.\n",
			out, len(m.Images), len(m.Charts), len(m.Models))
		return nil
	},
}

// socReleases reads the rendered security operations Applications from
// --cluster-dir and maps them onto --kubeaid-dir.
func socReleases(tenants []string) ([]siemctl.HelmRelease, error) {
	if bundleFlags.kubeaidDir == "" {
		return nil, fmt.Errorf("--kubeaid-dir is required: a KubeAid checkout at the chart revision")
	}
	dir := filepath.Join(clusterDir, "argocd-apps", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s (run 'kubeaid-cli siem render' first): %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		files = append(files, filepath.Join("argocd-apps", "templates", e.Name()))
	}
	apps, err := siemctl.ReadApplications(clusterDir, files)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{constants.ArgoCDAppSecurityOperations: true}
	for _, t := range tenants {
		want[constants.SecurityOperationsTenantNamespacePrefix+t] = true
	}
	var soc []siemctl.Application
	for _, a := range apps {
		if want[a.Name] {
			soc = append(soc, a)
			delete(want, a.Name)
		}
	}
	if len(want) > 0 {
		var missing []string
		for n := range want {
			missing = append(missing, n)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("not rendered in %s: %s (run 'kubeaid-cli siem render')", dir, strings.Join(missing, ", "))
	}
	return siemctl.HelmReleasesFromApps(soc, bundleFlags.kubeaidDir, clusterDir)
}

// BundleImportCmd is `siem bundle import`.
var BundleImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Push a bundle's images to a private registry",
	Long: `Unpacks a bundle and pushes each image to --registry, keeping its
repository path (docker.io/wazuh/wazuh-manager:4.14.3 becomes
<registry>/wazuh/wazuh-manager:4.14.3). Credentials come from
~/.docker/config.json. Point the nodes' container runtime at the registry as a
mirror (k3s: /etc/rancher/k3s/registries.yaml; containerd: hosts.toml), or set
the charts' image repositories to it. --extract-to keeps the charts, content and
models for the install.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		mapping, m, err := siemctl.ImportBundle(cmd.Context(), siemctl.ImportOptions{
			Bundle:    bundleFlags.bundle,
			Registry:  bundleFlags.registry,
			Insecure:  bundleFlags.insecure,
			ExtractTo: bundleFlags.extractTo,
			Log:       cmd.ErrOrStderr(),
		})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Pushed %d images of bundle %s to %s.\n", len(mapping), m.Version, bundleFlags.registry)
		return nil
	},
}

func init() {
	SiemCmd.AddCommand(BundleCmd)
	BundleCmd.AddCommand(BundleImportCmd)

	addClusterDirFlags(BundleCmd)
	f := BundleCmd.Flags()
	f.StringVar(&bundleFlags.kubeaidDir, "kubeaid-dir", "", "KubeAid checkout at the chart revision")
	f.StringVar(&bundleFlags.out, "out", "", "Bundle file. Default: kubesoc-<version>.tar")
	f.StringVar(&bundleFlags.version, "version", "", "Bundle version. Default: securityOperations.chartRevision")
	f.StringVar(&bundleFlags.content, "content", "", "Content package directory to include")
	f.StringVar(&bundleFlags.models, "ollama-models", "", "Ollama models directory (blobs/, manifests/) to include")
	f.StringVar(&bundleFlags.platform, "platform", "linux/amd64", "Image platform to pull")
	f.BoolVar(&bundleFlags.skipImages, "skip-images", false, "List the images in manifest.json without pulling them")
	f.StringSliceVar(&bundleFlags.extra, "extra-image", nil,
		"Image to add (repeatable): e.g. the CloudNativePG operator's default PostgreSQL image and mariadb-operator's MariaDB image, which the rendered CRs do not name")

	f = BundleImportCmd.Flags()
	f.StringVar(&bundleFlags.bundle, "bundle", "", "Bundle file")
	_ = BundleImportCmd.MarkFlagRequired("bundle")
	f.StringVar(&bundleFlags.registry, "registry", "", "Target registry and path prefix, e.g. registry.example.com/kubesoc")
	_ = BundleImportCmd.MarkFlagRequired("registry")
	f.BoolVar(&bundleFlags.insecure, "insecure", false, "Allow a plain HTTP or self-signed registry")
	f.StringVar(&bundleFlags.extractTo, "extract-to", "", "Keep the unpacked bundle in this directory")
}
