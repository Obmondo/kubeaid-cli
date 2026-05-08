// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"log/slog"
	"time"

	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	kubeaidScriptName  = "KubeAid Bootstrap Script"
	kubeaidScriptEmail = "info@obmondo.com"
)

// OperatorAttribution returns the Author signature and post-
// processed commit message kubeaid-cli should use when committing
// on the operator's behalf.
//
// When the operator has user.name + user.email set in their global
// git config (~/.gitconfig), the Author is the operator and a
// Co-Authored-By trailer credits the kubeaid-cli script — same
// pattern Git uses elsewhere for human-attributed-but-tool-driven
// commits (and what forges like Gitea/GitHub render as a co-author
// on the PR page). When global config is missing the script authors
// directly with no trailer; the trailer would just duplicate the
// Author line in that case.
//
// We read GlobalScope only — not Local. kubeaid-cli's commits land
// in the kubeaid-config repo, which the operator only ever interacts
// with via this script; there's no plausible reason for them to set
// a per-repo identity there. Reading Global also gives consistent
// attribution across multiple kubeaid-config repos managed from the
// same machine.
func OperatorAttribution(message string) (*object.Signature, string) {
	if cfg, err := goGitConfig.LoadConfig(goGitConfig.GlobalScope); err == nil {
		if cfg.User.Name != "" && cfg.User.Email != "" {
			return &object.Signature{
					Name:  cfg.User.Name,
					Email: cfg.User.Email,
					When:  time.Now(),
				}, message + "\n\nCo-Authored-By: " + kubeaidScriptName +
					" <" + kubeaidScriptEmail + ">"
		}
	} else {
		slog.Warn("Failed reading global git config; using KubeAid script identity",
			slog.Any("err", err))
	}
	return &object.Signature{
		Name:  kubeaidScriptName,
		Email: kubeaidScriptEmail,
		When:  time.Now(),
	}, message
}
