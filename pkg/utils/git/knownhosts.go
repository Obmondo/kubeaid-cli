// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"

	_ "embed"
)

// The hosts specified in this file, are know by defaut by ArgoCD.
// Because, we've picked them up from a argocd-ssh-known-hosts-cm ConfigMap 😉.
//
//go:embed templates/known_hosts
var CommonKnownHosts string

// Creates the known hosts file to be used by Go Git.
func createKnownHostsFile(ctx context.Context) {
	knownHosts := getKnownHosts()

	knownHostsFile, err := os.Create(constants.OutputPathKnownHostsFile)
	assert.AssertErrNil(ctx, err,
		"Failed creating known hosts file",
		slog.String("path", constants.OutputPathKnownHostsFile),
	)
	defer knownHostsFile.Close()

	_, err = knownHostsFile.WriteString(strings.Join(knownHosts, "\n") + "\n")
	assert.AssertErrNil(ctx, err, "Failed writing entries to known hosts file",
		slog.String("path", constants.OutputPathKnownHostsFile),
	)
}

// Returns a list of known hosts containing :
// known hosts of common Git repo hosting providers (like Azure DevOps, GitLab etc.), and
// any extra known hosts specified by the user.
func getKnownHosts() []string {
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
	knownHosts = append(knownHosts, config.ParsedGeneralConfig.Git.KnownHosts...)

	return knownHosts
}
