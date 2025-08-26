// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	_ "embed"

	v1 "k8s.io/api/core/v1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var (
	// REFER : https://kubernetes.io/docs/reference/command-line-tools-reference/kube-apiserver/.
	kubeAPIServerDefaultExtraArgsForAuditLogging = map[string]string{
		"audit-log-maxage":    "10",
		"audit-log-maxbackup": "1",
		"audit-log-maxsize":   "100",

		constants.KubeAPIServerFlagAuditPolicyFile: "/srv/kubernetes/audit.yaml",

		constants.KubeAPIServerFlagAuditLogPath: "/var/log/kube-apiserver-audit.logs",
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

	// If using the log backend, make sure that the log backend file is mounted to the Kube API
	// server pod.
	logBackendHostPath, ok := kubeAPIServerDefaultExtraArgsForAuditLogging[constants.KubeAPIServerFlagAuditLogPath]
	if ok {
		ensureHostPathGetsMounted(config.HostPathMountConfig{
			Name:      "log-backend",
			HostPath:  logBackendHostPath,
			MountPath: logBackendHostPath,
			PathType:  v1.HostPathFileOrCreate,
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
