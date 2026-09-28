// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// Where the SOC's sealed Secrets live in the cluster directory.
//
// They used to be rendered under sealed-secrets/<namespace>/, which the
// shared `secrets` Application syncs with prune and selfHeal: a bad merge
// there once pruned every SOC Secret at once. Now each SOC Application owns
// its own directory, security-operations/sealed-secrets/<namespace>/, as an
// extra source; the `secrets` app never sees it.
//
// Every sealed Secret the SOC renders must take its path from
// securityOperationsSealedSecretPath, so that it lands in the directory of
// the Application that owns its namespace.
const (
	// SecurityOperationsSealedSecretsDir is the SOC's sealed Secrets root,
	// relative to the cluster directory. One subdirectory per namespace, each
	// the extra source of the Application of that namespace.
	SecurityOperationsSealedSecretsDir = "security-operations/sealed-secrets"

	// securityOperationsLegacySealedSecretsDir is the shared `secrets` app's
	// directory the SOC rendered into before.
	securityOperationsLegacySealedSecretsDir = "sealed-secrets"
)

// securityOperationsSealedSecretAnnotations go on every SOC SealedSecret
// (the SealedSecret object itself, not its Secret template):
//
//   - Prune=false: no Application ever deletes it, neither the SOC app whose
//     tenant was removed nor the `secrets` app it moves away from (Argo CD
//     reads the option from the live object, so a SealedSecret carrying it
//     survives the migration whichever app applied it last).
//   - sync-wave -1: inside its Application, applied before the workloads that
//     read the Secret.
var securityOperationsSealedSecretAnnotations = [][2]string{
	{"argocd.argoproj.io/sync-options", "Prune=false"},
	{"argocd.argoproj.io/sync-wave", `"-1"`},
}

// securityOperationsSealedSecretPath is where the sealed Secret name in
// namespace lands, relative to the cluster directory.
func securityOperationsSealedSecretPath(namespace, name string) string {
	return path.Join(SecurityOperationsSealedSecretsDir, namespace, name+".yaml")
}

// securityOperationsLegacySealedSecretPath is where the sealed Secret was
// rendered before the SOC Applications owned their Secrets.
func securityOperationsLegacySealedSecretPath(namespace, name string) string {
	return path.Join(securityOperationsLegacySealedSecretsDir, namespace, name+".yaml")
}

// annotateSealedSecret adds securityOperationsSealedSecretAnnotations to the
// top-level metadata of a sealed Secret file (kubeseal's YAML output, with or
// without kubeaid's hash header). Idempotent; the ciphertext and the hash
// header are untouched, so SealIfPlaintextChanged keeps treating the file as
// current.
func annotateSealedSecret(raw []byte) ([]byte, error) {
	lines := strings.SplitAfter(string(raw), "\n")

	metadata := -1
	for i, line := range lines {
		if strings.TrimRight(line, "\r\n") == "metadata:" {
			metadata = i
			break
		}
	}
	if metadata < 0 {
		return nil, errors.New("no top-level metadata in the sealed Secret")
	}

	// The metadata block: the indented lines after "metadata:".
	end := metadata + 1
	annotations := -1
	for ; end < len(lines); end++ {
		line := lines[end]
		if line == "" || !strings.HasPrefix(line, " ") {
			break
		}
		if strings.TrimRight(line, "\r\n") == "  annotations:" {
			annotations = end
		}
	}

	var missing []string
	for _, kv := range securityOperationsSealedSecretAnnotations {
		present := false
		if annotations >= 0 {
			for _, line := range lines[annotations+1 : end] {
				if strings.HasPrefix(line, "    "+kv[0]+":") {
					present = true
					break
				}
			}
		}
		if !present {
			missing = append(missing, "    "+kv[0]+": "+kv[1]+"\n")
		}
	}
	if len(missing) == 0 {
		return raw, nil
	}

	insertAt := metadata + 1
	if annotations >= 0 {
		insertAt = annotations + 1
	} else {
		missing = append([]string{"  annotations:\n"}, missing...)
	}

	out := make([]string, 0, len(lines)+len(missing))
	out = append(out, lines[:insertAt]...)
	out = append(out, missing...)
	out = append(out, lines[insertAt:]...)
	return []byte(strings.Join(out, "")), nil
}

// annotateSealedSecretFile applies annotateSealedSecret to a file in place,
// writing only when something changed.
func annotateSealedSecretFile(filePath string) error {
	raw, err := os.ReadFile(filePath) //nolint:gosec // G304: a path kubeaid-cli rendered.
	if err != nil {
		return err
	}
	annotated, err := annotateSealedSecret(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", filePath, err)
	}
	if bytes.Equal(raw, annotated) {
		return nil
	}
	return os.WriteFile(filePath, annotated, 0o600) //nolint:gosec // G703: a path kubeaid-cli rendered.
}

// adoptLegacySealedSecret moves one sealed Secret towards its owned
// location without re-encrypting it: when the owned file is missing and the
// legacy one exists, the legacy file is annotated in place (so the live
// object carries Prune=false before it ever leaves the `secrets` app) and
// copied, ciphertext and hash header included, to the owned path. The legacy
// file stays: RemoveSecurityOperationsLegacySealedSecrets drops it once the
// SOC Application owns the object. Returns the paths written, relative to
// clusterDir.
func adoptLegacySealedSecret(clusterDir, namespace, name string) ([]string, error) {
	owned := securityOperationsSealedSecretPath(namespace, name)
	legacy := securityOperationsLegacySealedSecretPath(namespace, name)
	if fileExists(path.Join(clusterDir, owned)) || !fileExists(path.Join(clusterDir, legacy)) {
		return nil, nil
	}

	legacyPath := path.Join(clusterDir, legacy)
	if err := annotateSealedSecretFile(legacyPath); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(legacyPath) //nolint:gosec // G304: a path kubeaid-cli rendered.
	if err != nil {
		return nil, err
	}
	ownedPath := path.Join(clusterDir, owned)
	if err := os.MkdirAll(path.Dir(ownedPath), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(ownedPath, raw, 0o600); err != nil { //nolint:gosec // G703: a path kubeaid-cli rendered.
		return nil, err
	}
	return []string{legacy, owned}, nil
}

// syncLegacySealedSecret overwrites a still present legacy copy with the
// owned file just sealed, so the `secrets` app and the SOC app apply the same
// SealedSecret during the migration. Returns the legacy path when written.
func syncLegacySealedSecret(clusterDir, namespace, name string) ([]string, error) {
	legacy := securityOperationsLegacySealedSecretPath(namespace, name)
	legacyPath := path.Join(clusterDir, legacy)
	if !fileExists(legacyPath) {
		return nil, nil
	}
	raw, err := os.ReadFile( //nolint:gosec // G304: a path kubeaid-cli rendered.
		path.Join(clusterDir, securityOperationsSealedSecretPath(namespace, name)))
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(legacyPath) //nolint:gosec // G304: a path kubeaid-cli rendered.
	if err != nil {
		return nil, err
	}
	if bytes.Equal(raw, current) {
		return nil, nil
	}
	return []string{legacy}, os.WriteFile(legacyPath, raw, 0o600) //nolint:gosec // G703: a path kubeaid-cli rendered.
}

// SecurityOperationsLegacySealedSecretFiles lists the SOC's sealed Secrets
// still present in the shared sealed-secrets/ directory, relative to
// clusterDir, sorted.
func SecurityOperationsLegacySealedSecretFiles(clusterDir string) []string {
	var legacy []string
	for _, secret := range securityOperationsSecretFiles() {
		p := securityOperationsLegacySealedSecretPath(secret.Data.Namespace, secret.Data.Name)
		if fileExists(path.Join(clusterDir, p)) {
			legacy = append(legacy, p)
		}
	}
	sort.Strings(legacy)
	return legacy
}

// RemoveSecurityOperationsLegacySealedSecrets deletes the legacy copies of
// the SOC's sealed Secrets (and namespace directories left empty). It is step
// two of the migration and only safe once the SOC Applications synced their
// owned copies (see the `siem render` help): it refuses to remove a legacy
// file whose owned copy is missing. Returns the removed paths.
func RemoveSecurityOperationsLegacySealedSecrets(clusterDir string) ([]string, error) {
	var removed []string
	for _, secret := range securityOperationsSecretFiles() {
		ns, name := secret.Data.Namespace, secret.Data.Name
		legacy := securityOperationsLegacySealedSecretPath(ns, name)
		if !fileExists(path.Join(clusterDir, legacy)) {
			continue
		}
		if !fileExists(path.Join(clusterDir, securityOperationsSealedSecretPath(ns, name))) {
			return removed, fmt.Errorf("not removing %s: %s does not exist yet, render first",
				legacy, securityOperationsSealedSecretPath(ns, name))
		}
		if err := os.Remove(path.Join(clusterDir, legacy)); err != nil {
			return removed, err
		}
		removed = append(removed, legacy)

		// Drop the namespace directory once nothing else is in it; other
		// hand-sealed Secrets of that namespace keep it.
		dir := path.Join(clusterDir, securityOperationsLegacySealedSecretsDir, ns)
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return removed, nil
}

// SecurityOperationsApplicationsInSyncOrder returns the SOC Applications in
// the order they must sync: security-operations (it creates the tenant
// namespaces and the central search) first, then each wazuh-<code>. The
// rendered argocd.argoproj.io/sync-wave annotations encode the same order.
func SecurityOperationsApplicationsInSyncOrder() []string {
	if !config.SecurityOperationsEnabled() {
		return nil
	}
	apps := []string{constants.ArgoCDAppSecurityOperations}
	for _, tenant := range config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants {
		apps = append(apps, constants.SecurityOperationsTenantNamespacePrefix+tenant.Code)
	}
	return apps
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
