// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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

func TestAuditLoggingMounts(t *testing.T) {
	originalParsedGeneralConfig := config.ParsedGeneralConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = originalParsedGeneralConfig
	})

	findVolume := func(name string) (config.HostPathMountConfig, bool) {
		for _, volume := range config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes {
			if volume.Name == name {
				return volume, true
			}
		}

		return config.HostPathMountConfig{}, false
	}

	t.Run("mounts the audit-log directory (not the file), writable", func(t *testing.T) {
		resetParsedGeneralConfig()

		hydrateWithAuditLoggingOptions()

		volume, ok := findVolume("audit-log")
		if !ok {
			t.Fatal("expected an audit-log volume mount")
		}

		// The mount must be the parent directory so rotated backups persist,
		// not the audit.log file itself.
		if volume.HostPath != "/var/log/kubernetes/audit" {
			t.Fatalf("expected audit-log mount at /var/log/kubernetes/audit, got %q", volume.HostPath)
		}

		if volume.PathType != v1.HostPathDirectoryOrCreate {
			t.Fatalf("expected DirectoryOrCreate, got %q", volume.PathType)
		}

		if volume.ReadOnly {
			t.Fatal("audit-log mount must be writable (apiserver writes the log)")
		}
	})

	t.Run("mounts the policy file read-only", func(t *testing.T) {
		resetParsedGeneralConfig()

		hydrateWithAuditLoggingOptions()

		volume, ok := findVolume(constants.KubeAPIServerFlagAuditPolicyFile)
		if !ok {
			t.Fatal("expected an audit policy file volume mount")
		}

		if volume.HostPath != "/etc/kubernetes/audit-policy.yaml" {
			t.Fatalf("expected policy mount at /etc/kubernetes/audit-policy.yaml, got %q", volume.HostPath)
		}

		if volume.PathType != v1.HostPathFileOrCreate {
			t.Fatalf("expected FileOrCreate, got %q", volume.PathType)
		}

		if !volume.ReadOnly {
			t.Fatal("policy file mount must be read-only")
		}
	})

	t.Run("audit-log to stdout skips the directory mount", func(t *testing.T) {
		resetParsedGeneralConfig()
		config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[constants.KubeAPIServerFlagAuditLogPath] = "-"

		hydrateWithAuditLoggingOptions()

		if _, ok := findVolume("audit-log"); ok {
			t.Fatal("audit-log mount must be skipped when logging to stdout")
		}
	})
}
