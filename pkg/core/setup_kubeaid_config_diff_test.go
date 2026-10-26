// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffLines(t *testing.T) {
	testCases := []struct {
		name             string
		old              string
		new              string
		wantLines        []string
		wantAbsent       []string
		wantEmpty        bool
		wantTrimmed      bool
		wantChangedLines int
	}{
		{
			name:       "a reverted value shows both sides",
			old:        "cert-manager:\n  prometheus:\n    enabled: false\n",
			new:        "cert-manager:\n  prometheus:\n    enabled: true\n",
			wantLines:  []string{"-    enabled: false", "+    enabled: true"},
			wantAbsent: []string{"-cert-manager:", "+cert-manager:"},
		},
		{
			name:      "a deleted block shows only the removal",
			old:       "cilium:\n  nodePort:\n    directRoutingDevice: enp8s0.4003\n",
			new:       "cilium:\n",
			wantLines: []string{"-  nodePort:", "-    directRoutingDevice: enp8s0.4003"},
		},
		{
			name:      "identical content reports nothing",
			old:       "a: 1\n",
			new:       "a: 1\n",
			wantEmpty: true,
		},
		{
			name:       "a file that did not exist yet shows only additions",
			old:        "",
			new:        "cilium:\n  nodePort:\n    directRoutingDevice: enp8s0.4003\n",
			wantLines:  []string{"+cilium:", "+  nodePort:", "+    directRoutingDevice: enp8s0.4003"},
			wantAbsent: []string{"-"},
		},
		{
			name:       "a file emptied by the template shows only removals",
			old:        "cert-manager:\n  prometheus:\n    enabled: false\n",
			new:        "",
			wantLines:  []string{"-cert-manager:", "-  prometheus:", "-    enabled: false"},
			wantAbsent: []string{"+"},
		},
		{
			// The write still happens (bytes.Equal is false), so the warning
			// is logged with an empty diff.
			name:      "a trailing-newline-only change reports nothing",
			old:       "a: 1",
			new:       "a: 1\n",
			wantEmpty: true,
		},
		{
			name:             "a wholly different file is truncated at 20 changed lines",
			old:              strings.Repeat("old\n", 100),
			new:              strings.Repeat("new\n", 100),
			wantTrimmed:      true,
			wantChangedLines: 40,
		},
		{
			name:             "a long addition is capped at 20 lines too",
			old:              "",
			new:              strings.Repeat("new\n", 100),
			wantTrimmed:      true,
			wantChangedLines: 20,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := diffLines([]byte(testCase.old), []byte(testCase.new))

			if testCase.wantEmpty {
				assert.Empty(t, got)
				return
			}
			for _, want := range testCase.wantLines {
				assert.Contains(t, got, want)
			}
			for _, absent := range testCase.wantAbsent {
				assert.NotContains(t, got, absent)
			}
			if testCase.wantTrimmed {
				assert.Contains(t, got, "... (truncated)")
			}
			if testCase.wantChangedLines > 0 {
				changed := 0
				for _, line := range strings.Split(got, "\n") {
					if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
						changed++
					}
				}
				assert.Equal(t, testCase.wantChangedLines, changed)
			}
		})
	}
}
