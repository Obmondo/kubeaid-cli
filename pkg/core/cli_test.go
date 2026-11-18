// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// withFreshGeneralConfig swaps ParsedGeneralConfig for the duration of fn
// so the test never leaks the package-level config state to other tests.
func withFreshGeneralConfig(t *testing.T, fn func()) {
	t.Helper()

	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}

	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	fn()
}

// withCLIGlobals gives the test its own parsed config on the Hetzner cloud provider and clears the
// CLI's derived globals, restoring everything afterwards.
func withCLIGlobals(t *testing.T) {
	t.Helper()

	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	origProvider, origConfig, origCluster := globals.CloudProviderName, globals.Config, globals.Cluster
	t.Cleanup(func() {
		config.ParsedGeneralConfig, config.ParsedSecretsConfig = origGeneral, origSecrets
		globals.CloudProviderName, globals.Config, globals.Cluster = origProvider, origConfig, origCluster
	})

	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedSecretsConfig = &config.SecretsConfig{}
	globals.CloudProviderName = constants.CloudProviderHetzner
	globals.Config = nil
	globals.Cluster = nil
}

func TestCLIConfig(t *testing.T) {
	t.Run("is built over the parsed config when the parser did not build one", func(t *testing.T) {
		withCLIGlobals(t)

		cfg := cliConfig()

		assert.Same(t, config.ParsedGeneralConfig, cfg.General)
		assert.Same(t, config.ParsedSecretsConfig, cfg.Secrets)
		assert.Equal(t, constants.CloudProviderHetzner, cfg.CloudProviderName)
		assert.Equal(t, constants.DefaultCAPIClusterNamespace, cfg.CAPINamespace())
		assert.Same(t, cfg, globals.Config)
	})

	t.Run("keeps the config the parser built, so that provisioned state survives", func(t *testing.T) {
		withCLIGlobals(t)
		parsed, err := config.New(
			&config.GeneralConfig{Cloud: config.CloudConfig{Hetzner: &config.HetznerConfig{}}},
			&config.SecretsConfig{},
		)
		require.NoError(t, err)
		config.ParsedGeneralConfig, config.ParsedSecretsConfig = parsed.General, parsed.Secrets
		globals.Config = parsed
		parsed.Provisioned.ControlPlaneLBPrivateIP = "10.0.0.5"

		cfg := cliConfig()

		assert.Same(t, parsed, cfg)
		assert.Equal(t, "10.0.0.5", cfg.Provisioned.ControlPlaneLBPrivateIP)
	})

	t.Run("is rebuilt when the parsed config moved on", func(t *testing.T) {
		withCLIGlobals(t)
		first := cliConfig()

		config.ParsedGeneralConfig = &config.GeneralConfig{}
		second := cliConfig()

		assert.NotSame(t, first, second)
		assert.Same(t, config.ParsedGeneralConfig, second.General)
	})
}

func TestKubeCluster(t *testing.T) {
	t.Run("is one instance for as long as the config stays", func(t *testing.T) {
		withCLIGlobals(t)

		first := kubeCluster()

		assert.Same(t, first, kubeCluster())
		assert.Same(t, cliConfig(), first.Config())
	})

	t.Run("follows the config when it is rebuilt", func(t *testing.T) {
		withCLIGlobals(t)
		first := kubeCluster()

		config.ParsedGeneralConfig = &config.GeneralConfig{}

		assert.NotSame(t, first, kubeCluster())
	})

	t.Run("is the cluster the parser built, when it matches", func(t *testing.T) {
		withCLIGlobals(t)
		cluster, err := kubernetes.NewCluster(cliConfig())
		require.NoError(t, err)
		globals.Cluster = cluster

		assert.Same(t, cluster, kubeCluster())
	})
}

func TestGitFailure(t *testing.T) {
	cause := errors.New("reference not found")
	refAttr := slog.String("ref", "v9.9.9")
	withoutCause := &git.Error{Message: "Failed cloning repo"}

	tests := []struct {
		name        string
		err         error
		wantMessage string
		wantAttrs   []any
		wantCause   error
	}{
		{
			name:        "keeps the headline, the attributes and the cause",
			err:         &git.Error{Message: "Failed resolving provided git ref", Err: cause, Attrs: []slog.Attr{refAttr}},
			wantMessage: "Failed resolving provided git ref",
			wantAttrs:   []any{refAttr},
			wantCause:   cause,
		},
		{
			name:        "finds a git error under a wrapper",
			err:         fmt.Errorf("syncing: %w", &git.Error{Message: "Failed cloning repo", Err: cause}),
			wantMessage: "Failed cloning repo",
			wantCause:   cause,
		},
		{
			name:        "takes a git error without a cause as its own cause",
			err:         withoutCause,
			wantMessage: "Failed cloning repo",
			wantCause:   withoutCause,
		},
		{
			name:        "gives any other error the generic headline",
			err:         cause,
			wantMessage: "Git operation failed",
			wantCause:   cause,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message, attrs, got := gitFailure(tc.err)

			assert.Equal(t, tc.wantMessage, message)
			assert.Equal(t, tc.wantAttrs, attrs)
			assert.Equal(t, tc.wantCause, got)
		})
	}
}

func TestTemplateFailure(t *testing.T) {
	cause := errors.New("template: unexpected EOF")
	templateAttr := slog.String("template", "templates/argocd-apps/values-cilium.yaml.tmpl")

	tests := []struct {
		name        string
		err         error
		wantMessage string
		wantAttrs   []any
	}{
		{
			name:        "reading",
			err:         &templateUtils.Error{Stage: templateUtils.StageReading, Template: templateAttr.Value.String(), Err: cause},
			wantMessage: "Failed getting template from embedded file-system",
			wantAttrs:   []any{templateAttr},
		},
		{
			name:        "parsing",
			err:         &templateUtils.Error{Stage: templateUtils.StageParsing, Template: templateAttr.Value.String(), Err: cause},
			wantMessage: "Failed parsing template",
			wantAttrs:   []any{templateAttr},
		},
		{
			name:        "executing",
			err:         &templateUtils.Error{Stage: templateUtils.StageExecuting, Template: templateAttr.Value.String(), Err: cause},
			wantMessage: "Failed executing template",
			wantAttrs:   []any{templateAttr},
		},
		{
			name:        "under a wrapper",
			err:         fmt.Errorf("rendering: %w", &templateUtils.Error{Stage: templateUtils.StageParsing, Template: templateAttr.Value.String(), Err: cause}),
			wantMessage: "Failed parsing template",
			wantAttrs:   []any{templateAttr},
		},
		{
			name:        "any other error",
			err:         cause,
			wantMessage: "Failed rendering template",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message, attrs, got := templateFailure(tc.err)

			assert.Equal(t, tc.wantMessage, message)
			assert.Equal(t, tc.wantAttrs, attrs)
			assert.Equal(t, cause, got)
		})
	}
}
