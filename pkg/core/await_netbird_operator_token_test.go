// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	coreV1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

	// Force the non-interactive path so the test hits the poll (and the
	// cancelled ctx) deterministically — regardless of whether `go test` is
	// run from a real terminal.
	prevTTY := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = prevTTY })

	fakeClient := crFake.NewClientBuilder().WithScheme(newPostgresTestScheme(t)).Build()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := captureStdout(t, func() {
		if _, err := awaitNetBirdOperatorToken(ctx, fakeClient, "s3cret-from-cluster"); err == nil {
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

// netBirdVPNTestConfig installs a managed-Keycloak VPN cluster config (which
// makes netBirdOperatorEnabled() true) for the duration of the test.
func netBirdVPNTestConfig(t *testing.T) {
	t.Helper()
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
}

// forceInteractive makes stdinIsTerminal report true and installs the given
// stubs for the choice/token prompts, restoring all three on cleanup.
func forceInteractive(
	t *testing.T,
	choice netBirdTokenChoice,
	token string,
) {
	t.Helper()
	prevTTY, prevChoice, prevToken := stdinIsTerminal, promptNetBirdTokenChoice, readNetBirdToken
	stdinIsTerminal = func() bool { return true }
	promptNetBirdTokenChoice = func() (netBirdTokenChoice, error) { return choice, nil }
	readNetBirdToken = func() (string, error) { return token, nil }
	t.Cleanup(func() {
		stdinIsTerminal, promptNetBirdTokenChoice, readNetBirdToken = prevTTY, prevChoice, prevToken
	})
}

// TestAwaitNetBirdOperatorToken_PasteNowCreatesSecret verifies the paste-now
// path: kubeaid-cli creates the netbird-mgmt-api-key Secret from the pasted
// token and returns proceedWithLockdown=true.
func TestAwaitNetBirdOperatorToken_PasteNowCreatesSecret(t *testing.T) {
	netBirdVPNTestConfig(t)
	forceInteractive(t, netBirdTokenPasteNow, "pasted-pat")

	fakeClient := crFake.NewClientBuilder().WithScheme(newPostgresTestScheme(t)).Build()

	var proceed bool
	out := captureStdout(t, func() {
		var err error
		proceed, err = awaitNetBirdOperatorToken(context.Background(), fakeClient, "s3cret-from-cluster")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !proceed {
		t.Error("expected proceedWithLockdown=true after pasting the token")
	}

	secret := &coreV1.Secret{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Namespace: netBirdOperatorSecretNamespace,
		Name:      netBirdOperatorSecretName,
	}, secret); err != nil {
		t.Fatalf("expected the Secret to exist after paste-now: %v", err)
	}
	if got := string(secret.Data[netBirdOperatorSecretKey]); got != "pasted-pat" {
		t.Errorf("%s = %q, want %q", netBirdOperatorSecretKey, got, "pasted-pat")
	}
	if !strings.Contains(out, "one-off") {
		t.Errorf("expected the one-off persistence note in output, got:\n%s", out)
	}
}

// TestAwaitNetBirdOperatorToken_DeferSkipsLockdown verifies the defer path:
// no Secret is created, proceedWithLockdown=false, and the "left reachable"
// box prints.
func TestAwaitNetBirdOperatorToken_DeferSkipsLockdown(t *testing.T) {
	netBirdVPNTestConfig(t)
	forceInteractive(t, netBirdTokenDefer, "")

	fakeClient := crFake.NewClientBuilder().WithScheme(newPostgresTestScheme(t)).Build()

	var proceed bool
	out := captureStdout(t, func() {
		var err error
		proceed, err = awaitNetBirdOperatorToken(context.Background(), fakeClient, "s3cret-from-cluster")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if proceed {
		t.Error("expected proceedWithLockdown=false when the operator defers")
	}
	if !strings.Contains(out, "cluster left reachable") {
		t.Errorf("expected the deferred box in output, got:\n%s", out)
	}

	secret := &coreV1.Secret{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{
		Namespace: netBirdOperatorSecretNamespace,
		Name:      netBirdOperatorSecretName,
	}, secret)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected no Secret to be created on defer, Get returned: %v", err)
	}
}

// TestCreateNetBirdOperatorSecret verifies the Secret is written with the
// NB_API_KEY value in the expected namespace/name.
func TestCreateNetBirdOperatorSecret(t *testing.T) {
	fakeClient := crFake.NewClientBuilder().WithScheme(newPostgresTestScheme(t)).Build()

	if err := createNetBirdOperatorSecret(context.Background(), fakeClient, "the-pat"); err != nil {
		t.Fatalf("createNetBirdOperatorSecret: %v", err)
	}

	secret := &coreV1.Secret{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Namespace: netBirdOperatorSecretNamespace,
		Name:      netBirdOperatorSecretName,
	}, secret); err != nil {
		t.Fatalf("expected the Secret to exist: %v", err)
	}
	if got := string(secret.Data[netBirdOperatorSecretKey]); got != "the-pat" {
		t.Errorf("%s = %q, want %q", netBirdOperatorSecretKey, got, "the-pat")
	}
}
