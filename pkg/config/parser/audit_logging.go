// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	_ "embed"
	"path"

	v1 "k8s.io/api/core/v1"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

var (
	// REFER : https://kubernetes.io/docs/reference/command-line-tools-reference/kube-apiserver/.
	kubeAPIServerDefaultExtraArgsForAuditLogging = map[string]string{
		"audit-log-maxage":    "10",
		"audit-log-maxbackup": "1",
		"audit-log-maxsize":   "100",

		constants.KubeAPIServerFlagAuditPolicyFile: "/etc/kubernetes/audit-policy.yaml",

		constants.KubeAPIServerFlagAuditLogPath: "/var/log/kubernetes/audit/audit.log",
	}

	//go:embed audit-policy.yaml
	defaultAuditPolicy string
)

// Hydrates the KubeAid Bootstrap Script config with the default Kube API audit logging related
// options (if not provided by the user).
func hydrateWithAuditLoggingOptions() {
	if !config.ParsedGeneralConfig.Cluster.EnableAuditLogging {
		return
	}

	// If the user has not specified required extra args for the Kube API server, then use the
	// default values.
	for key, defaultValue := range kubeAPIServerDefaultExtraArgsForAuditLogging {
		if _, ok := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[key]; !ok {
			config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[key] = defaultValue
		}
	}

	auditPolicyFileHostPath := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[constants.KubeAPIServerFlagAuditPolicyFile]

	// If the user has not specified an Audit Policy file, then use the default one.
	{
		isAuditPolicyFileProvidedByUser := false
		for _, file := range config.ParsedGeneralConfig.Cluster.APIServer.Files {
			if file.Path == auditPolicyFileHostPath {
				isAuditPolicyFileProvidedByUser = true
				break
			}
		}

		if !isAuditPolicyFileProvidedByUser {
			config.ParsedGeneralConfig.Cluster.APIServer.Files = append(
				config.ParsedGeneralConfig.Cluster.APIServer.Files,
				config.FileConfig{
					Path:    auditPolicyFileHostPath,
					Content: defaultAuditPolicy,
				},
			)
		}
	}

	// Make sure that the audit policy file is mounted to the Kube API server pod.
	ensureHostPathGetsMounted(config.HostPathMountConfig{
		Name:      constants.KubeAPIServerFlagAuditPolicyFile,
		HostPath:  auditPolicyFileHostPath,
		MountPath: auditPolicyFileHostPath,
		ReadOnly:  true,
		PathType:  v1.HostPathFileOrCreate,
	})

	// Mount the audit-log directory (not just the file) into the apiserver
	// static pod. With rotation enabled (audit-log-maxbackup), kube-apiserver
	// writes the active log plus rotated backups side by side, so the whole
	// directory must be host-backed for them to persist. Derive it from the
	// effective audit-log-path so an operator override moves the mount with
	// it, and skip the mount when audit goes to stdout ("-") or the value is
	// otherwise not an absolute file path.
	auditLogPath := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[constants.KubeAPIServerFlagAuditLogPath]
	if path.IsAbs(auditLogPath) {
		auditLogDir := path.Dir(auditLogPath)
		ensureHostPathGetsMounted(config.HostPathMountConfig{
			Name:      "audit-log",
			HostPath:  auditLogDir,
			MountPath: auditLogDir,
			ReadOnly:  false,
			PathType:  v1.HostPathDirectoryOrCreate,
		})
	}
}

// Ensures that the given host path gets mounted to the Kube API server pod. If not, then uses the
// given default volume to do the mount.
func ensureHostPathGetsMounted(volume config.HostPathMountConfig) {
	hostPathAlreadyMounted := false
	for _, userSpecifiedVolume := range config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes {
		if userSpecifiedVolume.HostPath == volume.HostPath {
			hostPathAlreadyMounted = true
			break
		}
	}

	if !hostPathAlreadyMounted {
		config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes = append(
			config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes,
			volume,
		)
	}
}
