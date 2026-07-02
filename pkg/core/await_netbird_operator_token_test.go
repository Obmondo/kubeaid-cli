// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to it. Needed because printKeycloakUserSetupForNetBird
// writes via fmt.Print (printNextStepsBox), not an injectable writer.
// Mirrors the os.Pipe capture in pkg/utils/kubernetes/certificates_test.go.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return string(out)
}

// TestPrintKeycloakUserSetupForNetBird_Gating verifies the panel only
// prints on a VPN cluster with managed Keycloak and a non-empty Keycloak
// DNS, and that the password row falls back to the kubectl command when no
// live password was supplied.
func TestPrintKeycloakUserSetupForNetBird_Gating(t *testing.T) {
	tests := []struct {
		name         string
		clusterType  string
		keycloak     *config.KeycloakConfig
		password     string
		wantEmpty    bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:        "no-op: not a VPN cluster",
			clusterType: constants.ClusterTypeWorkload,
			keycloak:    &config.KeycloakConfig{Mode: constants.KeycloakModeManaged, DNS: "keycloak.acme.com"},
			wantEmpty:   true,
		},
		{
			name:        "no-op: VPN cluster with external (non-managed) Keycloak",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: "external", DNS: "keycloak.acme.com"},
			wantEmpty:   true,
		},
		{
			name:        "no-op: VPN + managed Keycloak but empty DNS",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: constants.KeycloakModeManaged, DNS: ""},
			wantEmpty:   true,
		},
		{
			name:        "no-op: VPN cluster without a keycloak block",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    nil,
			wantEmpty:   true,
		},
		{
			name:        "prints with the live password inline",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: constants.KeycloakModeManaged, DNS: "keycloak.acme.com", Realm: "acme"},
			password:    "s3cret-from-cluster",
			wantContains: []string{
				"Create your Keycloak login first",
				"https://keycloak.acme.com/auth/admin/",
				"s3cret-from-cluster",
			},
			wantAbsent: []string{"kubectl"},
		},
		{
			name:        "prints the kubectl fallback when the password is empty",
			clusterType: constants.ClusterTypeVPN,
			keycloak:    &config.KeycloakConfig{Mode: constants.KeycloakModeManaged, DNS: "keycloak.acme.com", Realm: "acme"},
			password:    "",
			wantContains: []string{
				"Create your Keycloak login first",
				"kubectl",
				"base64 -d",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := config.ParsedGeneralConfig
			config.ParsedGeneralConfig = &config.GeneralConfig{}
			config.ParsedGeneralConfig.Cluster.Type = tc.clusterType
			config.ParsedGeneralConfig.Cluster.Keycloak = tc.keycloak
			t.Cleanup(func() { config.ParsedGeneralConfig = orig })

			out := captureStdout(t, func() {
				printKeycloakUserSetupForNetBird(tc.password)
			})

			if tc.wantEmpty {
				if out != "" {
					t.Fatalf("expected no output, got:\n%s", out)
				}
				return
			}

			for _, want := range tc.wantContains {
				if !strings.Contains(out, want) {
					t.Fatalf("expected output to contain %q, got:\n%s", want, out)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(out, absent) {
					t.Fatalf("expected output NOT to contain %q, got:\n%s", absent, out)
				}
			}
		})
	}
}

// TestAwaitNetBirdOperatorToken_KeycloakBoxBeforeNetBirdBox verifies the
// ordering: the "create your Keycloak login first" box must print before
// the "NetBird operator API key required" box, since signing in to the
// NetBird dashboard is Keycloak SSO. Uses a fake client with no
// netbird-mgmt-api-key Secret (Get returns NotFound, matching a fresh
// cluster) and a pre-cancelled context so waitForNetBirdOperatorSecret's
// select falls into ctx.Done() on its first iteration instead of blocking
// up to 30 minutes — both boxes are already printed by the time it's reached.
func TestAwaitNetBirdOperatorToken_KeycloakBoxBeforeNetBirdBox(t *testing.T) {
	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
	config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
		Mode:  constants.KeycloakModeManaged,
		DNS:   "keycloak.acme.com",
		Realm: "acme",
	}
	config.ParsedGeneralConfig.Cluster.NetBird = &config.NetBirdConfig{DNS: "netbird.acme.com"}
	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	fakeClient := crFake.NewClientBuilder().WithScheme(newPostgresTestScheme(t)).Build()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := captureStdout(t, func() {
		if err := awaitNetBirdOperatorToken(ctx, fakeClient, "s3cret-from-cluster"); err == nil {
			t.Error("expected an error once the cancelled context aborts the wait, got nil")
		}
	})

	idxKeycloak := strings.Index(out, "Create your Keycloak login first")
	idxNetBird := strings.Index(out, "NetBird operator API key required")

	if idxKeycloak < 0 {
		t.Fatalf("expected the Keycloak login box in output, got:\n%s", out)
	}
	if idxNetBird < 0 {
		t.Fatalf("expected the NetBird operator API key box in output, got:\n%s", out)
	}
	if idxKeycloak >= idxNetBird {
		t.Fatalf("expected the Keycloak login box BEFORE the NetBird box; got Keycloak at %d, NetBird at %d:\n%s",
			idxKeycloak, idxNetBird, out)
	}
}
