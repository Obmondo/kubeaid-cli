package config

import (
	_ "embed"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	v1 "k8s.io/api/core/v1"
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

	//go:embed files/defaults/audit-policy.yaml
	defaultAuditPolicy string
)

// Hydrates the KubeAid Bootstrap Script config with the default Kube API audit logging related
// options (if not provided by the user).
func hydrateWithAuditLoggingOptions() {
	if !ParsedConfig.Cluster.EnableAuditLogging {
		return
	}

	// If the user has not specified required extra args for the Kube API server, then use the
	// default values.
	for key, defaultValue := range kubeAPIServerDefaultExtraArgsForAuditLogging {
		if _, ok := ParsedConfig.Cluster.APIServer.ExtraArgs[key]; !ok {
			ParsedConfig.Cluster.APIServer.ExtraArgs[key] = defaultValue
		}
	}

	auditPolicyFileHostPath := ParsedConfig.Cluster.APIServer.ExtraArgs[constants.KubeAPIServerFlagAuditPolicyFile]

	// If the user has not specified an Audit Policy file, then use the default one.
	{
		isAuditPolicyFileProvidedByUser := false
		for _, file := range ParsedConfig.Cluster.APIServer.Files {
			if file.Path == auditPolicyFileHostPath {
				isAuditPolicyFileProvidedByUser = true
				break
			}
		}

		if !isAuditPolicyFileProvidedByUser {
			ParsedConfig.Cluster.APIServer.Files = append(ParsedConfig.Cluster.APIServer.Files, FileConfig{
				Path:    auditPolicyFileHostPath,
				Content: defaultAuditPolicy,
			})
		}
	}

	// Make sure that the audit policy file is mounted to the Kube API server pod.
	ensureHostPathGetsMounted(HostPathMountConfig{
		Name:      constants.KubeAPIServerFlagAuditPolicyFile,
		HostPath:  auditPolicyFileHostPath,
		MountPath: auditPolicyFileHostPath,
		ReadOnly:  true,
		PathType:  v1.HostPathFileOrCreate,
	})

	// If using the log backend, make sure that the log backend file is mounted to the Kube API
	// server pod.
	if logBackendHostPath, ok := kubeAPIServerDefaultExtraArgsForAuditLogging[constants.KubeAPIServerFlagAuditLogPath]; ok {
		ensureHostPathGetsMounted(HostPathMountConfig{
			Name:      "log-backend",
			HostPath:  logBackendHostPath,
			MountPath: logBackendHostPath,
			PathType:  v1.HostPathFileOrCreate,
		})
	}
}

// Ensures that the given host path gets mounted to the Kube API server pod. If not, then uses the
// given default volume to do the mount.
func ensureHostPathGetsMounted(volume HostPathMountConfig) {
	hostPathAlreadyMounted := false
	for _, userSpecifiedVolume := range ParsedConfig.Cluster.APIServer.ExtraVolumes {
		if userSpecifiedVolume.HostPath == volume.HostPath {
			hostPathAlreadyMounted = true
			break
		}
	}

	if !hostPathAlreadyMounted {
		ParsedConfig.Cluster.APIServer.ExtraVolumes = append(ParsedConfig.Cluster.APIServer.ExtraVolumes, volume)
	}
}
