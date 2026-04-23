// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestArgoCDHelmValues(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(dir string)
		wantNil    bool
		wantSuffix string
	}{
		{
			name:    "returns nil when values-argocd.yaml does not exist",
			setup:   func(_ string) {},
			wantNil: true,
		},
		{
			name: "returns ValueFiles pointing at the rendered file when present",
			setup: func(dir string) {
				sub := filepath.Join(dir, "argocd-apps")
				if err := os.MkdirAll(sub, 0o750); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				payload := []byte(
					"---\nconfigs:\n  ssh:\n    knownHosts: |\n" +
						"      gitea.example.com ssh-ed25519 AAA\n",
				)
				if err := os.WriteFile(filepath.Join(sub, "values-argocd.yaml"), payload, 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			wantNil:    false,
			wantSuffix: "argocd-apps/values-argocd.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(dir)

			got := argoCDHelmValues(context.Background(), dir)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("argoCDHelmValues() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("argoCDHelmValues() = nil, want non-nil")
			}
			want := filepath.Join(dir, tc.wantSuffix)
			if len(got.ValueFiles) != 1 || got.ValueFiles[0] != want {
				t.Errorf("ValueFiles = %v, want [%s]", got.ValueFiles, want)
			}
		})
	}
}
