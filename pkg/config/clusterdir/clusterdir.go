// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package clusterdir owns the per-cluster config directory convention:
// <user config dir>/kubeaid-cli/<cluster>/configs, which is
// ~/.config/kubeaid-cli/<cluster>/configs on Linux.
//
// Deliberately NOT the working-directory-relative "outputs/configs" that
// --configs-directory defaults to. secrets.yaml holds cloud credentials and
// an mTLS private key, and an operator running kubeaid-cli inside a checkout
// — the likely case, since they have a kubeaid-config repo — would leave
// those one `git add .` away from being committed.
//
// Stdlib-only on purpose: both pkg/obmondo and pkg/config/parser depend on
// this, and neither should have to pull in the other.
package clusterdir

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// dirName is the per-user root every cluster's config directory sits under.
const dirName = "kubeaid-cli"

// configsSubdir keeps the config files in their own directory so a cluster's
// root stays free for anything else that later belongs to it.
const configsSubdir = "configs"

// generalFileName is what marks a directory as holding a real config, rather
// than one an interrupted run left behind.
const generalFileName = "general.yaml"

// ReservedLogsName is the one directory under the per-user root that is not
// a cluster: the shared logs directory (see SharedLogsDir).
const ReservedLogsName = "logs"

// nameShape is what may own a directory under the per-user root: letters of
// either case, digits, hyphens and underscores, starting and ending
// alphanumeric. Domain-label-shaped, because the cluster name has to work
// as an FQDN label (<cluster-name>.svc.local) — but deliberately looser
// than the interactive prompt's lowercase RFC-1123 rule for new clusters:
// clusters predating that rule carry names like "Legacy_Prod", and this
// gate must keep them operable rather than orphan their directories.
var nameShape = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_-]*[A-Za-z0-9])?$`)

// maxClusterNameLen is the DNS label limit.
const maxClusterNameLen = 63

// ValidateName reports whether clusterName may own a directory under the
// per-user root. Every path builder here calls it, so an invalid name is
// rejected wherever it enters — a flag, a hand-edited general.yaml, or the
// prompt.
func ValidateName(clusterName string) error {
	if clusterName == "" {
		return fmt.Errorf("cluster name is empty")
	}
	// Case-folded: on the case-insensitive filesystems macOS and Windows
	// ship with, "Logs" IS the shared logs directory.
	if strings.EqualFold(clusterName, ReservedLogsName) {
		return fmt.Errorf(
			"%q is reserved for the shared logs directory and cannot be a cluster name",
			ReservedLogsName,
		)
	}
	if len(clusterName) > maxClusterNameLen {
		return fmt.Errorf("cluster name must be at most %d characters", maxClusterNameLen)
	}
	if !nameShape.MatchString(clusterName) {
		return fmt.Errorf(
			"cluster name %q must be letters, digits, '-' or '_', starting and ending alphanumeric (no dots or path separators)",
			clusterName,
		)
	}
	return nil
}

// Home returns clusterName's directory itself. Configs live in a
// subdirectory of it (see For); a run's outputs — kubeconfigs, logs, the
// generated k3d config — sit next to them, so everything the CLI knows
// about a cluster has one home. Kubeconfigs and logs are strictly state,
// which XDG would put under ~/.local/state — kept here deliberately, so a
// cluster is one directory rather than two trees to correlate.
func Home(clusterName string) (string, error) {
	if err := ValidateName(clusterName); err != nil {
		return "", err
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(base, dirName, clusterName), nil
}

// For returns where clusterName's config lives.
func For(clusterName string) (string, error) {
	home, err := Home(clusterName)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configsSubdir), nil
}

// logsSubdir is where a cluster's run logs live inside its home. Not to be
// confused with ReservedLogsName, the shared pre-parse logs directory that
// sits directly under the per-user root.
const logsSubdir = "logs"

// LogsDir returns clusterName's logs directory — the one definition of
// that path, so the log-placement call sites cannot drift from each other.
func LogsDir(clusterName string) (string, error) {
	home, err := Home(clusterName)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, logsSubdir), nil
}

// LogsDirForConfigs returns where a run whose configs live in
// configsDirectory keeps its logs: the owning cluster's logs directory when
// the path is a cluster's configs directory under the per-user root, and
// the "logs" subdirectory of configsDirectory itself otherwise — the
// operator chose that location, so everything the run produces goes there.
// The one definition of this routing, shared by the pre-parse log placement
// and config generate's post-write relocation.
func LogsDirForConfigs(configsDirectory string) string {
	if owner := OwnerOfConfigs(configsDirectory); owner != "" {
		if dir, err := LogsDir(owner); err == nil {
			return dir
		}
	}
	return filepath.Join(configsDirectory, logsSubdir)
}

// OwnerOfConfigs returns the name of the cluster whose configs directory
// path is, and "" when path is not a per-cluster configs directory (or no
// per-user root can be located). The pre-parse half of the path rule: it
// answers from the path alone, so a run can route its log file before
// general.yaml has been read.
func OwnerOfConfigs(path string) string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}

	clean := filepath.Clean(path)
	if filepath.Base(clean) != configsSubdir {
		return ""
	}

	home := filepath.Dir(clean)
	if filepath.Dir(home) != filepath.Join(base, dirName) {
		return ""
	}

	name := filepath.Base(home)
	if ValidateName(name) != nil {
		return ""
	}
	return name
}

// SharedLogsDir returns the logs directory used before a run has resolved
// which cluster it concerns — and permanently, for runs that never do
// (version, an aborted config generate). Once the cluster is known, the
// run's log file moves into <cluster home>/logs.
//
// "logs" is therefore a reserved name no cluster may take; ValidateName
// enforces that on every path builder.
func SharedLogsDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(base, dirName, ReservedLogsName), nil
}

// List returns the names of every cluster with a config on disk, sorted.
//
// Best-effort: an unreadable or absent root is an empty list, not an error.
// Callers use this to enrich a message the operator is already reading, so
// failing to build it must never replace the message it was decorating.
func List() []string {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil
	}

	entries, err := os.ReadDir(filepath.Join(base, dirName))
	if err != nil {
		return nil
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// A name the path builders would refuse cannot be operated on —
		// listing it would hand the operator a --cluster-name the very
		// next command rejects. (Also skips the shared logs directory.)
		if ValidateName(entry.Name()) != nil {
			continue
		}
		// A directory only counts once it actually holds a config — an
		// aborted run can leave the tree behind with nothing in it.
		general := filepath.Join(base, dirName, entry.Name(), configsSubdir, generalFileName)
		if _, err := os.Stat(general); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}

	sort.Strings(names)
	return names
}
