// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

// autoDetectedConfig holds values resolved automatically without user interaction.
type autoDetectedConfig struct {
	K8sVersion            string
	KubeAidVersion        string
	KubePrometheusVersion string
	SSHAgentAvail         bool
}

// autoDetect resolves K8s version (latest-1 minor), KubeAid version
// (latest stable release), and checks SSH agent availability.
//
// Runs silently — the slog handler's stdout level is LevelError, so
// the per-step slog.Info calls inside the detect* helpers land in
// the log file only. The K8s version surfaces in the Step 0 profile
// picker (the auto-detected row is starred); the KubeAid tag
// surfaces in the configuration-summary box; the SSH agent state
// drives whether Step 4 asks for a key path. Nothing needs to be
// drawn to the TTY between `kubeaid-cli cluster bootstrap` and the
// first interactive prompt — earlier revisions opened four empty
// progress-bar step panels here that printed `● Detecting X` /
// `────` headers with no bodies and clashed with the spinner.
func autoDetect() *autoDetectedConfig {
	cfg := &autoDetectedConfig{}
	cfg.K8sVersion = detectK8sVersion()
	cfg.KubePrometheusVersion = detectKubePrometheusVersion(cfg.K8sVersion)
	cfg.KubeAidVersion = detectKubeAidVersion()
	cfg.SSHAgentAvail = detectSSHAgent()
	return cfg
}

// detectK8sVersion fetches the latest stable K8s version and returns the latest patch
// of the previous minor version (latest-1).
func detectK8sVersion() string {
	slog.Info("Auto-detecting K8s version (latest-1 minor)")

	latestStable, err := fetchURL(constants.K8sReleaseAPIURL)
	if err != nil {
		slog.Warn(
			"Failed fetching latest K8s version, skipping auto-detect",
			slog.Any("error", err),
		)
		return ""
	}
	latestStable = strings.TrimSpace(latestStable)

	// Parse the minor version from e.g. "v1.35.1".
	minor, err := parseMinorVersion(latestStable)
	if err != nil {
		slog.Warn("Failed parsing K8s minor version", slog.Any("error", err))
		return ""
	}

	if minor <= 0 {
		slog.Warn("K8s minor version is 0, cannot compute latest-1")
		return ""
	}

	// Fetch the latest patch of the previous minor.
	prevMinorURL := fmt.Sprintf(constants.K8sStableMinorURLFmt, minor-1)
	prevMinorVersion, err := fetchURL(prevMinorURL)
	if err != nil {
		slog.Warn("Failed fetching K8s previous minor version", slog.Any("error", err))
		return ""
	}
	prevMinorVersion = strings.TrimSpace(prevMinorVersion)

	slog.Info("Auto-detected K8s version", slog.String("version", prevMinorVersion))
	return prevMinorVersion
}

// detectKubePrometheusVersion picks the latest compatible kube-prometheus version
// for the given K8s version using the compatibility matrix.
func detectKubePrometheusVersion(k8sVersion string) string {
	if k8sVersion == "" {
		return ""
	}

	minor, err := parseMinorVersion(k8sVersion)
	if err != nil {
		slog.Warn("Failed parsing K8s minor for kube-prometheus lookup",
			slog.Any("error", err))
		return ""
	}

	key := fmt.Sprintf("v1.%d", minor)
	versions, ok := constants.KubernetesKubePrometheusVersionCompatibilityMatrix[key]
	if !ok || len(versions) == 0 {
		slog.Warn("No compatible kube-prometheus version found",
			slog.String("k8s", key))
		return ""
	}

	// Pick the last entry (highest version) in the list.
	selected := versions[len(versions)-1]
	slog.Info("Auto-detected kube-prometheus version",
		slog.String("version", selected))
	return selected
}

// parseMinorVersion extracts the minor version number from a semver string like "v1.35.1".
func parseMinorVersion(version string) (int, error) {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, fmt.Errorf("unexpected version format: %s", version)
	}
	return strconv.Atoi(parts[1])
}

// releaseInfo represents a GitHub release API response entry.
type releaseInfo struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// detectKubeAidVersion fetches KubeAid releases and returns the latest stable release tag.
func detectKubeAidVersion() string {
	slog.Info("Auto-detecting KubeAid version (latest stable release)")

	url := constants.KubeAidReleasesAPIURL + "?per_page=10"

	var releases []releaseInfo
	if err := utils.FetchJSON(url, &releases); err != nil {
		slog.Warn("Failed fetching KubeAid releases", slog.Any("error", err))
		return ""
	}

	// Return the first stable release (latest).
	for _, r := range releases {
		if r.Prerelease || r.Draft {
			continue
		}
		slog.Info("Auto-detected KubeAid version", slog.String("version", r.TagName))
		return r.TagName
	}

	slog.Warn("Could not find any stable KubeAid release")
	return ""
}

// detectSSHAgent checks whether an SSH agent is available and has keys loaded.
func detectSSHAgent() bool {
	sock := os.Getenv(constants.EnvNameSSHAuthSock)
	if sock == "" {
		slog.Info("SSH_AUTH_SOCK not set, SSH agent not available")
		return false
	}

	// Verify the socket exists and is reachable.
	conn, err := net.Dial("unix", sock) //nolint:gosec // sock is the user's own SSH_AUTH_SOCK
	if err != nil {
		slog.Info("SSH agent socket not reachable", slog.Any("error", err))
		return false
	}
	_ = conn.Close()

	// Check if the agent has keys loaded.
	out, err := exec.Command("ssh-add", "-l").CombinedOutput()
	if err != nil {
		slog.Info("SSH agent has no keys loaded", slog.String("output", string(out)))
		return false
	}

	slog.Info("SSH agent available with keys loaded")
	return true
}

// fetchURL performs an HTTP GET and returns the response body as a string.
func fetchURL(url string) (string, error) {
	body, err := utils.FetchURLBytes(url)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
