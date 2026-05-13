// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildK8sProfiles_HappyPath(t *testing.T) {
	t.Parallel()

	// current minor = 36, latestPerCycle covers the two prior
	// minors → all four profiles resolve cleanly.
	latest := map[string]string{
		"1.34": "1.34.7",
		"1.35": "1.35.4",
		"1.36": "1.36.0",
	}

	got := buildK8sProfiles(36, latest)

	assert.Len(t, got, 4)

	assert.Equal(t, "third", got[0].Name)
	assert.Equal(t, "Proven", got[0].Label)
	assert.Equal(t, "v1.34.7", got[0].Version)
	assert.False(t, got[0].Disabled)

	assert.Equal(t, "second", got[1].Name)
	assert.Equal(t, "Balanced", got[1].Label)
	assert.Equal(t, "v1.35.4", got[1].Version)
	assert.False(t, got[1].Disabled)

	assert.Equal(t, "after-first-dot", got[2].Name)
	assert.Equal(t, "Early Adopter", got[2].Label)
	assert.Equal(t, "v1.36.1", got[2].Version)

	assert.Equal(t, "latest", got[3].Name)
	assert.Equal(t, "Bleeding Edge", got[3].Label)
	assert.Equal(t, "v1.36.0", got[3].Version)
}

func TestBuildK8sProfiles_MissingPriorMinors_DisablesProvenAndBalanced(t *testing.T) {
	t.Parallel()

	// EOL data only carries the current cycle — operator's machine
	// has stale embedded data that hasn't caught up to the live
	// release. Proven + Balanced should be marked disabled with a
	// note; Early Adopter + Bleeding Edge still resolve from
	// current minor alone.
	latest := map[string]string{
		"1.36": "1.36.0",
	}

	got := buildK8sProfiles(36, latest)

	assert.True(t, got[0].Disabled, "Proven should be disabled with no 1.34 entry")
	assert.Equal(t, "no EOL entry", got[0].Note)

	assert.True(t, got[1].Disabled, "Balanced should be disabled with no 1.35 entry")
	assert.Equal(t, "no EOL entry", got[1].Note)

	assert.False(t, got[2].Disabled)
	assert.Equal(t, "v1.36.1", got[2].Version)

	assert.False(t, got[3].Disabled)
	assert.Equal(t, "v1.36.0", got[3].Version)
}

func TestDefaultPickedProfile_PrefersBalanced(t *testing.T) {
	t.Parallel()

	profiles := buildK8sProfiles(36, map[string]string{
		"1.34": "1.34.7",
		"1.35": "1.35.4",
		"1.36": "1.36.0",
	})

	assert.Equal(t, "second", defaultPickedProfile(profiles))
}

func TestDefaultPickedProfile_FallsThroughWhenBalancedDisabled(t *testing.T) {
	t.Parallel()

	// No 1.35 entry — Balanced is disabled. Default should fall
	// through to the next non-disabled profile (Proven, the first
	// in table order).
	profiles := buildK8sProfiles(36, map[string]string{
		"1.34": "1.34.7",
		"1.36": "1.36.0",
	})

	assert.Equal(t, "third", defaultPickedProfile(profiles))
}

func TestDefaultPickedProfile_AllDisabled_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	// Pathological: no EOL entries at all → only the two
	// current-minor profiles are usable, but in this test we
	// force all to disabled to exercise the fallback.
	profiles := []K8sProfile{
		{Name: "third", Disabled: true},
		{Name: "second", Disabled: true},
		{Name: "after-first-dot", Disabled: true},
		{Name: "latest", Disabled: true},
	}

	assert.Equal(t, "", defaultPickedProfile(profiles))
}

func TestResolveCurrentK8sMinor_LiveOverridesEOL(t *testing.T) {
	t.Parallel()

	// detected.K8sVersion is "latest-1 minor" — live autodetect
	// resolved it as v1.35.4. Embedded EOL data only goes up to
	// 1.34. Live should win → currentMinor = 36.
	detected := &autoDetectedConfig{K8sVersion: "v1.35.4"}
	latest := map[string]string{"1.33": "1.33.11", "1.34": "1.34.7"}

	got, source := resolveCurrentK8sMinor(detected, latest)
	assert.Equal(t, 36, got)
	assert.Equal(t, k8sCurrentSourceLive, source)
}

func TestResolveCurrentK8sMinor_FallbackToEOLOnEmptyLive(t *testing.T) {
	t.Parallel()

	// detected.K8sVersion empty (network failed during autodetect).
	// Use the highest cycle in embedded EOL as current.
	detected := &autoDetectedConfig{K8sVersion: ""}
	latest := map[string]string{
		"1.33": "1.33.11",
		"1.34": "1.34.7",
		"1.35": "1.35.4",
	}

	got, source := resolveCurrentK8sMinor(detected, latest)
	assert.Equal(t, 35, got)
	assert.Equal(t, k8sCurrentSourceEOL, source)
}

func TestResolveCurrentK8sMinor_NoDataAtAll_ReturnsZero(t *testing.T) {
	t.Parallel()

	got, source := resolveCurrentK8sMinor(&autoDetectedConfig{}, map[string]string{})
	assert.Equal(t, 0, got)
	assert.Equal(t, "", source)
}

func TestResolveCurrentK8sMinor_LiveParseFailure_FallsBackToEOL(t *testing.T) {
	t.Parallel()

	// detected.K8sVersion has a non-semver shape — the live parse
	// fails, fallback to EOL.
	detected := &autoDetectedConfig{K8sVersion: "garbage"}
	latest := map[string]string{"1.34": "1.34.7"}

	got, source := resolveCurrentK8sMinor(detected, latest)
	assert.Equal(t, 34, got)
	assert.Equal(t, k8sCurrentSourceEOL, source)
}
