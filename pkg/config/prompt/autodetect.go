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

	"github.com/charmbracelet/lipgloss"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

// autoDetectedConfig holds values resolved automatically without user interaction.
type autoDetectedConfig struct {
	K8sVersion            string
	KubeAidVersion        string
	KubePrometheusVersion string
	SSHAgentAvail         bool
}

// autoDetect resolves K8s version (latest-1 minor), KubeAid version (latest stable release),
// and checks SSH agent availability. Shows a progress bar during detection.
func autoDetect() *autoDetectedConfig {
	bar := progress.New("Detecting K8s version (latest-1)")

	cfg := &autoDetectedConfig{}

	cfg.K8sVersion = detectK8sVersion()

	bar.Describe("Detecting KubePrometheus version")
	cfg.KubePrometheusVersion = detectKubePrometheusVersion(cfg.K8sVersion)

	bar.Describe("Detecting KubeAid version (latest)")
	cfg.KubeAidVersion = detectKubeAidVersion()

	bar.Describe("Checking SSH agent")
	cfg.SSHAgentAvail = detectSSHAgent()

	bar.Finish()

	// Surface the KubeAid release tag up front. K8s is owned by
	// the Step 0 profile picker; SSH agent state drives whether
	// Step 4 asks for a key path (no need to print). KubeAid's
	// tag is the only one that's locked in at this point and the
	// operator needs to see it before any prompt fires — that
	// version determines which kubeaid-config templates this run
	// will render.
	if cfg.KubeAidVersion != "" {
		printKubeAidReleaseTag(cfg.KubeAidVersion)
	}

	return cfg
}

// printKubeAidReleaseTag renders the locked-in KubeAid tag as a
// visually prominent banner so the operator notices it even when
// scrolling fast. lipgloss handles falling back to plain text on
// terminals without colour support.
func printKubeAidReleaseTag(tag string) {
	label := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#02BF87")).
		Render("KubeAid")

	version := lipgloss.NewStyle().
		Bold(true).
		Render(tag)

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Italic(true).
		Render("(latest stable release)")

	fmt.Printf("  %s  %s  %s\n", label, version, hint)
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
