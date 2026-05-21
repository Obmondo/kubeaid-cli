// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/version"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

//go:embed k8s-eol.json
var k8sEOLData []byte

const (
	k8sEOLDateLayout = "2006-01-02"
	nearEOLWindow    = 90 * 24 * time.Hour
)

var (
	nowFn                    = time.Now
	lifecyclesFn             = loadK8sLifecycles
	k8sReleaseAPIURL         = constants.K8sReleaseAPIURL
	latestStableK8sReleaseFn = latestStableK8sRelease
)

type k8sLifecycle struct {
	Cycle       string    `json:"cycle"`
	ReleaseDate string    `json:"releaseDate"`
	Support     maybeDate `json:"support"`
	EOL         string    `json:"eol"`
	Latest      string    `json:"latest"`
}

// K8sLatestPerCycle returns a snapshot of the embedded EOL data as
// a map of cycle string ("1.35") to the latest known patch version
// ("1.35.4"). The prompt package's K8s profile picker uses this to
// resolve concrete versions per profile when dl.k8s.io is unreachable
// or to seed the "patch level" of the latest two minor releases.
func K8sLatestPerCycle() (map[string]string, error) {
	entries, err := lifecyclesFn()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for cycle, e := range entries {
		out[cycle] = e.Latest
	}
	return out, nil
}

// LatestStableK8sRelease re-exports the embedded fetch wrapper so
// the prompt package can probe dl.k8s.io without duplicating the
// HTTP boilerplate. Empty string + non-nil error on transport
// failure — caller is expected to fall back to embedded EOL data.
func LatestStableK8sRelease() (string, error) {
	return latestStableK8sReleaseFn()
}

type maybeDate string

func (m *maybeDate) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("false")) ||
		bytes.Equal(data, []byte("null")) {
		*m = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*m = maybeDate(s)
	return nil
}

func loadK8sLifecycles() (map[string]k8sLifecycle, error) {
	return parseK8sLifecycles(k8sEOLData)
}

func parseK8sLifecycles(data []byte) (map[string]k8sLifecycle, error) {
	var entries []k8sLifecycle
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing K8s EOL data: %w", err)
	}

	byCycle := make(map[string]k8sLifecycle, len(entries))
	for _, e := range entries {
		byCycle[e.Cycle] = e
	}
	return byCycle, nil
}

func latestStableK8sRelease() (string, error) {
	body, err := utils.FetchURLBytes(k8sReleaseAPIURL)
	if err != nil {
		return "", utils.WrapError("fetching latest stable K8s release", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func checkK8sNotReleased(k8sVersion string) error {
	latestRaw, err := latestStableK8sReleaseFn()
	if err != nil {
		return utils.WrapError("fetching latest stable K8s release", err)
	}

	parsed, err := version.ParseSemantic(k8sVersion)
	if err != nil {
		return utils.WrapError("parsing provided K8s version", err)
	}

	latest, err := version.ParseSemantic(latestRaw)
	if err != nil {
		return utils.WrapError("parsing latest stable K8s version", err)
	}

	if parsed.GreaterThan(latest) {
		return fmt.Errorf(
			"the provided k8s version is not released: %s is greater than the latest stable release %s",
			k8sVersion, latestRaw,
		)
	}
	return nil
}

func checkK8sLifecycle(ctx context.Context, k8sVersion string) error {
	semver := version.MustParseSemantic(k8sVersion)
	cycle := fmt.Sprintf("%d.%d", semver.Major(), semver.Minor())

	entries, err := lifecyclesFn()
	if err != nil {
		return err
	}

	entry, ok := entries[cycle]
	if !ok {
		slog.InfoContext(ctx,
			"K8s minor not present in embedded EOL table; skipping lifecycle check",
			slog.String("cycle", cycle),
		)
		return fmt.Errorf("provided k8s version %s is not supported by kubeaid-cli", k8sVersion)
	}

	now := nowFn()

	eol, err := time.Parse(k8sEOLDateLayout, entry.EOL)
	if err != nil {
		return fmt.Errorf("parsing EOL date %q for K8s %s: %w", entry.EOL, cycle, err)
	}

	if now.After(eol) {
		return fmt.Errorf("K8s %s reached EOL on %s — pick a supported version", cycle, entry.EOL)
	}

	if entry.Support != "" {
		support, err := time.Parse(k8sEOLDateLayout, string(entry.Support))
		if err != nil {
			return fmt.Errorf("parsing support-end date %q for K8s %s: %w", entry.Support, cycle, err)
		}

		if now.Before(support) && support.Sub(now) <= nearEOLWindow {
			slog.WarnContext(ctx,
				fmt.Sprintf("K8s %s leaves active support on %s; consider upgrading", cycle, entry.Support),
			)
		}
	}

	if now.Before(eol) && eol.Sub(now) <= nearEOLWindow {
		slog.WarnContext(ctx,
			fmt.Sprintf("K8s %s reaches EOL on %s", cycle, entry.EOL),
		)
	}

	return nil
}
