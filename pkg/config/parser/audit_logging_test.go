// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"testing"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func resetParsedGeneralConfig() {
	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedGeneralConfig.Cluster.EnableAuditLogging = true
	config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs = map[string]string{}
}

func TestAuditLoggingDefaultOptions(t *testing.T) {
	originalParsedGeneralConfig := config.ParsedGeneralConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = originalParsedGeneralConfig
	})

	t.Run("sets default audit API server args", func(t *testing.T) {
		resetParsedGeneralConfig()

		hydrateWithAuditLoggingOptions()

		for key, defaultValue := range kubeAPIServerDefaultExtraArgsForAuditLogging {
			value, found := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[key]
			if !found {
				t.Fatalf("expected default extra arg %q to be present", key)
			}

			if value != defaultValue {
				t.Fatalf(
					"expected default extra arg %q to have value %q, got %q",
					key,
					defaultValue,
					value,
				)
			}
		}
	})

	t.Run("sets default audit policy file when user does not provide one", func(t *testing.T) {
		resetParsedGeneralConfig()

		hydrateWithAuditLoggingOptions()

		auditPolicyFilePath := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[constants.KubeAPIServerFlagAuditPolicyFile]

		foundPolicyFile := false
		for _, file := range config.ParsedGeneralConfig.Cluster.APIServer.Files {
			if file.Path != auditPolicyFilePath {
				continue
			}

			foundPolicyFile = true
			if file.Content != defaultAuditPolicy {
				t.Fatalf("expected default audit policy content for path %q", auditPolicyFilePath)
			}
		}

		if !foundPolicyFile {
			t.Fatalf("expected audit policy file to be present at path %q", auditPolicyFilePath)
		}
	})
}
