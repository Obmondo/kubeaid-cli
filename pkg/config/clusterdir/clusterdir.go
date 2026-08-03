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
	"sort"
)

// dirName is the per-user root every cluster's config directory sits under.
const dirName = "kubeaid-cli"

// configsSubdir keeps the config files in their own directory so a cluster's
// root stays free for anything else that later belongs to it.
const configsSubdir = "configs"

// generalFileName is what marks a directory as holding a real config, rather
// than one an interrupted run left behind.
const generalFileName = "general.yaml"

// For returns where clusterName's config lives.
func For(clusterName string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(base, dirName, clusterName, configsSubdir), nil
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
