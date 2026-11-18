// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"

	_ "embed"
)

// The hosts specified in this file, are know by defaut by ArgoCD.
// Because, we've picked them up from a argocd-ssh-known-hosts-cm ConfigMap 😉.
//
//go:embed templates/known_hosts
var CommonKnownHosts string

// Creates the known hosts file to be used by Go Git.
func (c *Client) createKnownHostsFile() error {
	knownHosts := c.getKnownHosts()

	knownHostsFile, err := os.Create(constants.OutputPathKnownHostsFile)
	if err != nil {
		return fail("Failed creating known hosts file", err,
			slog.String("path", constants.OutputPathKnownHostsFile))
	}
	defer knownHostsFile.Close()

	_, err = knownHostsFile.WriteString(strings.Join(knownHosts, "\n") + "\n")
	if err != nil {
		return fail("Failed writing entries to known hosts file", err,
			slog.String("path", constants.OutputPathKnownHostsFile))
	}

	return nil
}

// Returns a list of known hosts containing :
// known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.), and
// any extra known hosts specified by the user.
func (c *Client) getKnownHosts() []string {
	knownHosts := []string{}

	// Add known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.).
	for line := range strings.Lines(CommonKnownHosts) {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		knownHosts = append(knownHosts, line)
	}

	// Add extra known hosts provided by the user.
	knownHosts = append(knownHosts, c.general().Git.KnownHosts...)

	return knownHosts
}
