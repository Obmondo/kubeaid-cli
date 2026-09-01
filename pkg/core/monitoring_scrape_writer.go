// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

// scrapeNamespacesFieldPattern matches the prometheus_scrape_namespaces list in a
// *-vars.jsonnet file, capturing its (possibly empty, possibly multi-line) contents between the
// brackets. Tolerates both the freshly-generated "[]" shape (see cluster-vars.jsonnet.tmpl) and
// a hand-edited multi-line list (one quoted namespace per line, as every existing cluster's vars
// file already has it).
var scrapeNamespacesFieldPattern = regexp.MustCompile(
	`(?s)(prometheus_scrape_namespaces:\s*\[)(.*?)(\])`,
)

// quotedStringPattern matches a single- or double-quoted jsonnet string literal, capturing its
// contents. jsonnet accepts both quote styles and this codebase uses both across clusters (e.g.
// double-quoted on master's vpn-vars.jsonnet, single-quoted on the hetzner-qa branch's) - this is
// how existing list entries are read back regardless of which one a given file uses.
var quotedStringPattern = regexp.MustCompile(`["']([^"']*)["']`)

// AddMissingScrapeNamespaces appends any namespace missing prometheus-k8s RBAC to
// prometheus_scrape_namespaces in the cluster's *-vars.jsonnet, then regenerates the
// kube-prometheus manifests from that updated file.
//
// This is a surgical text edit of the EXISTING file, not a call to buildKubePrometheus /
// createFileFromTemplate — that path opens the destination with O_TRUNC and rewrites it from
// cluster-vars.jsonnet.tmpl unconditionally, which would silently wipe out every hand-edit an
// operator has made to the file since initial bootstrap (ingress hosts, resource limits,
// connect_obmondo, previously-added scrape namespaces, ...). Regenerating just the
// kube-prometheus/ manifests via runKubePrometheusBuilder is safe on repeat calls: it only
// derives output from the vars.jsonnet that's now on disk.
//
// Deliberately does NOT commit or push - this only touches the operator's local working tree.
// Publishing the change (review the diff, commit, push, let ArgoCD sync it) is left to the
// operator, same as any other GitOps change; kubeaid-cli doesn't push to a customer's config
// repo unattended.
func AddMissingScrapeNamespaces(ctx context.Context, missing []string) error {
	clusterDir := utils.GetClusterDir()
	jsonnetVarsFilePath := fmt.Sprintf(
		"%s/%s-vars.jsonnet",
		clusterDir,
		config.ParsedGeneralConfig.Cluster.Name,
	)

	original, err := os.ReadFile(jsonnetVarsFilePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", jsonnetVarsFilePath, err)
	}

	updated, changed, err := insertScrapeNamespaces(string(original), missing)
	if err != nil {
		return fmt.Errorf("updating prometheus_scrape_namespaces in %s: %w", jsonnetVarsFilePath, err)
	}
	if !changed {
		return nil
	}

	//nolint:gosec // vars.jsonnet is operator-authored config, not a secret - 0o644 matches its
	// existing on-disk permissions.
	if err := os.WriteFile(jsonnetVarsFilePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", jsonnetVarsFilePath, err)
	}

	kubeAidDir := utils.GetKubeAidDir()
	if err := runKubePrometheusBuilder(
		ctx, os.Getuid(), os.Getgid(), kubeAidDir, clusterDir, constants.KubePromBuilderImage,
	); err != nil {
		return fmt.Errorf("regenerating kube-prometheus manifests: %w", err)
	}

	return nil
}

// insertScrapeNamespaces returns vars.jsonnet's content with any namespace in missing that isn't
// already present appended to the prometheus_scrape_namespaces list, and whether it changed
// anything (namespaces already listed are left as-is, so this is safe to call repeatedly).
func insertScrapeNamespaces(content string, missing []string) (string, bool, error) {
	match := scrapeNamespacesFieldPattern.FindStringSubmatchIndex(content)
	if match == nil {
		return "", false, fmt.Errorf("prometheus_scrape_namespaces field not found")
	}

	existingBlock := content[match[4]:match[5]]

	existing := map[string]bool{}
	for _, submatch := range quotedStringPattern.FindAllStringSubmatch(existingBlock, -1) {
		existing[submatch[1]] = true
	}

	var toAdd []string
	for _, namespace := range missing {
		if !existing[namespace] {
			toAdd = append(toAdd, namespace)
		}
	}
	if len(toAdd) == 0 {
		return content, false, nil
	}

	var newEntries strings.Builder
	for _, namespace := range toAdd {
		fmt.Fprintf(&newEntries, "\n    %q,", namespace)
	}

	trimmedBlock := strings.TrimRight(existingBlock, " \t\n")
	replacement := trimmedBlock + newEntries.String() + "\n  "

	updated := content[:match[4]] + replacement + content[match[5]:]
	return updated, true, nil
}
