// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
	coreV1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// netBirdOperatorSecretName + namespace match the chart's
// secretKeyRef on the netbird-operator Deployment — the operator
// mounts NB_API_KEY from this Secret at startup and falls into
// CreateContainerConfigError when it doesn't exist.
const (
	netBirdOperatorSecretNamespace = "netbird"
	netBirdOperatorSecretName      = "netbird-mgmt-api-key"
	netBirdOperatorSecretKey       = "NB_API_KEY"
)

// awaitNetBirdOperatorToken handles the NetBird operator API-key gate. The
// netbird-operator can't start without the netbird-mgmt-api-key Secret, and its
// MutatingWebhookConfiguration on Pods (failurePolicy: Fail) then blocks every
// cluster-wide Pod create — so this must be settled before lockdown, while the
// control-plane LB's public interface is still up.
//
// Returns proceedWithLockdown: true means the mesh key is in place (or not
// needed) and the caller may lock the cluster down; false means the operator
// deferred NetBird setup, so the caller must SKIP lockdown and the LB
// public-interface disable and leave the cluster reachable (see
// printNetBirdSetupDeferred) — otherwise they'd be cut off from kube-apiserver
// with no mesh path back in.
//
// When the Secret is missing and stdin is a terminal, the operator chooses:
// paste the token now (kubeaid-cli creates the Secret), wait while they create
// it out-of-band (poll), or defer. Without a terminal (CI) it keeps the
// original poll-then-fail behaviour — there the Secret is expected via
// secrets.yaml, so a missing one is a misconfig worth surfacing.
//
// keycloakAdminPassword is the live Keycloak admin password (read before the LB
// is disabled), surfaced in the "create your Keycloak login first" box printed
// ahead of the NetBird steps since the dashboard login is Keycloak SSO. Empty
// triggers the kubectl-fetch fallback.
//
// No-op — returns (true, nil) — when the cluster doesn't host the
// netbird-operator (workload clusters without a keycloak block). When
// secrets.yaml carries netbird.apiKey the sealed Secret lands via the secrets
// app sync and the existence check passes without prompting.
func awaitNetBirdOperatorToken(
	ctx context.Context,
	clusterClient client.Client,
	keycloakAdminPassword string,
) (proceedWithLockdown bool, err error) {
	if !netBirdOperatorEnabled() {
		return true, nil
	}

	exists, err := netBirdOperatorSecretExists(ctx, clusterClient)
	if err != nil {
		return false, fmt.Errorf("checking %s/%s: %w",
			netBirdOperatorSecretNamespace, netBirdOperatorSecretName, err)
	}
	if exists {
		slog.InfoContext(ctx, "NetBird operator API-key Secret already present, skipping prompt",
			slog.String("namespace", netBirdOperatorSecretNamespace),
			slog.String("name", netBirdOperatorSecretName),
		)
		return true, nil
	}

	// Signing in to the NetBird dashboard (to mint the service-user PAT) is
	// Keycloak SSO — so print the Keycloak create-user box BEFORE the NetBird
	// steps. No-op on clusters without a managed Keycloak.
	printKeycloakUserSetupForNetBird(keycloakAdminPassword)
	printNetBirdOperatorInstructions(netbirdDashboardHost())

	// No TTY (CI / unattended): keep the original poll-then-fail behaviour. The
	// interactive chooser below needs a terminal, and in CI the Secret is meant
	// to arrive via secrets.yaml — silently skipping lockdown would be worse
	// than surfacing the misconfig.
	if !stdinIsTerminal() {
		if err := waitForNetBirdOperatorSecret(ctx, clusterClient); err != nil {
			return false, err
		}
		return true, nil
	}

	choice, err := promptNetBirdTokenChoice()
	if err != nil {
		return false, fmt.Errorf("NetBird API-key prompt: %w", err)
	}

	switch choice {
	case netBirdTokenPasteNow:
		token, err := readNetBirdToken()
		if err != nil {
			return false, fmt.Errorf("reading NetBird API token: %w", err)
		}
		if err := createNetBirdOperatorSecret(ctx, clusterClient, token); err != nil {
			return false, err
		}
		slog.InfoContext(ctx, "Created NetBird operator API-key Secret from the pasted token",
			slog.String("namespace", netBirdOperatorSecretNamespace),
			slog.String("name", netBirdOperatorSecretName),
		)
		printNetBirdSecretPersistenceNote()
		return true, nil

	case netBirdTokenWait:
		if err := waitForNetBirdOperatorSecret(ctx, clusterClient); err != nil {
			// A cancellation (Ctrl+C) aborts the run. A plain 30-minute timeout
			// does not: the cluster is provisioned, so leave it reachable and
			// let the operator finish and re-run, rather than lock them out.
			if ctx.Err() != nil {
				return false, err
			}
			slog.WarnContext(ctx,
				"Timed out waiting for the NetBird operator Secret; leaving the cluster reachable",
				slog.Any("err", err))
			printNetBirdSetupDeferred()
			return false, nil
		}
		return true, nil

	default: // netBirdTokenDefer
		printNetBirdSetupDeferred()
		return false, nil
	}
}

// netBirdTokenChoice is the operator's answer at the interactive NetBird
// API-key gate.
type netBirdTokenChoice int

const (
	// netBirdTokenPasteNow: paste the PAT now; kubeaid-cli creates the Secret.
	netBirdTokenPasteNow netBirdTokenChoice = iota
	// netBirdTokenWait: create it out-of-band; poll until the Secret appears.
	netBirdTokenWait
	// netBirdTokenDefer: set up NetBird later; skip lockdown, leave reachable.
	netBirdTokenDefer
)

// Test seams: the huh prompts and TTY check need a real terminal, so unit tests
// override these to drive the branching without one.
var (
	stdinIsTerminal          = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	promptNetBirdTokenChoice = runNetBirdTokenChoiceForm
	readNetBirdToken         = runNetBirdTokenInputForm
)

// runNetBirdTokenChoiceForm asks how the operator wants to satisfy the NetBird
// API-key requirement.
func runNetBirdTokenChoiceForm() (netBirdTokenChoice, error) {
	choice := netBirdTokenPasteNow
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[netBirdTokenChoice]().
				Title("NetBird operator API key required").
				Description("The netbird-operator can't start without it — how do you want to proceed?").
				Options(
					huh.NewOption("Paste the token now — kubeaid-cli creates the Secret and continues", netBirdTokenPasteNow),
					huh.NewOption("Wait here while I create it (secrets.yaml or kubectl)", netBirdTokenWait),
					huh.NewOption("Skip — set up NetBird later (cluster stays reachable, no lockdown)", netBirdTokenDefer),
				).
				Value(&choice),
		),
	).Run()
	return choice, err
}

// runNetBirdTokenInputForm reads the NetBird service-user PAT from the operator,
// masked and trimmed.
func runNetBirdTokenInputForm() (string, error) {
	var token string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Paste the NetBird service-user token (PAT)").
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("token must not be empty")
					}
					return nil
				}).
				Value(&token),
		),
	).Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(token), nil
}

// createNetBirdOperatorSecret writes the pasted PAT into the netbird-mgmt-api-key
// Secret so the operator pod can start. One-off: it does not persist to
// secrets.yaml — see printNetBirdSecretPersistenceNote.
func createNetBirdOperatorSecret(ctx context.Context, c client.Client, token string) error {
	secret := &coreV1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: netBirdOperatorSecretNamespace,
			Name:      netBirdOperatorSecretName,
		},
		Type: coreV1.SecretTypeOpaque,
		Data: map[string][]byte{netBirdOperatorSecretKey: []byte(token)},
	}
	if err := c.Create(ctx, secret); err != nil {
		return fmt.Errorf("creating Secret %s/%s: %w",
			netBirdOperatorSecretNamespace, netBirdOperatorSecretName, err)
	}
	return nil
}

// sealedSecretCommandLines returns the wrapped kubeseal one-liner that creates
// the netbird-mgmt-api-key as a SealedSecret — the git-safe, kubeaid-native way
// to persist the token: the sealed-secrets controller decrypts it into the real
// Secret, and the sealed manifest is safe to commit. indent is prepended to each
// line so callers can place it inside a next-steps box.
func sealedSecretCommandLines(indent string) []string {
	return []string{
		indent + "kubectl create secret generic " + netBirdOperatorSecretName +
			" -n " + netBirdOperatorSecretNamespace + " \\",
		indent + "  --dry-run=client --from-literal=" + netBirdOperatorSecretKey +
			"='<paste-token-here>' -o yaml \\",
		indent + "| kubeseal --namespace " + netBirdOperatorSecretNamespace + " \\",
		indent + "    --controller-namespace " + constants.NamespaceSealedSecrets + " \\",
		indent + "    --controller-name " + constants.SealedSecretsControllerName + " -o yaml \\",
		indent + "| kubectl apply -f -",
	}
}

// printNetBirdSecretPersistenceNote reminds the operator that the Secret
// kubeaid-cli just created is one-off (dropped on cluster re-creation) and shows
// the two durable ways to persist the token: secrets.yaml, or a SealedSecret.
func printNetBirdSecretPersistenceNote() {
	lines := []string{
		"",
		"  Created Secret " + netBirdOperatorSecretNamespace + "/" + netBirdOperatorSecretName + " from the pasted token.",
		"  It won't survive cluster re-creation. For a durable setup, either:",
		"",
		"  1. Add it to secrets.yaml and re-run kubeaid-cli:",
		"",
		"       netbird:",
		"         apiKey: <paste-token-here>",
		"",
		"  2. Seal it into the cluster (commit the output for git durability):",
		"",
	}
	lines = append(lines, sealedSecretCommandLines("       ")...)
	lines = append(lines, "")

	printNextStepsBox("NetBird API key saved (one-off)", lines)
}

// printNetBirdSetupDeferred tells the operator the cluster was left publicly
// reachable — host-firewall lockdown AND the control-plane LB public-interface
// disable are both skipped — because without the NetBird API key they'd have no
// mesh path back in. Re-running kubeaid-cli once the Secret exists completes
// lockdown.
func printNetBirdSetupDeferred() {
	printNextStepsBox("NetBird setup deferred — cluster left reachable", []string{
		"",
		"  No NetBird API key yet, so kubeaid-cli did NOT lock down the cluster:",
		"    - host firewall not applied",
		"    - control-plane LB public interface left up",
		"",
		"  This keeps kube-apiserver reachable while you finish NetBird setup;",
		"  locking down now would cut off your only way in (you're not on the",
		"  mesh yet).",
		"",
		"  When ready: create the " + netBirdOperatorSecretName + " Secret (secrets.yaml",
		"  netbird.apiKey, or kubectl create), then re-run kubeaid-cli to lock down.",
		"",
	})
}

// printKeycloakUserSetupForNetBird renders the "create your Keycloak login
// first" panel shown immediately before the NetBird operator API-key
// instructions. The NetBird dashboard login is Keycloak SSO, so the
// operator must have a realm user before they can sign in to mint the
// service-user PAT.
//
// No-op unless this cluster hosts a managed Keycloak (VPN cluster with
// managed Keycloak) — only then does the admin console + admin credential
// this panel surfaces exist locally. keycloakAdminPassword is the live
// admin password read before the control-plane LB was disabled; empty
// falls back to the kubectl-fetch command (same logic as the final panel).
func printKeycloakUserSetupForNetBird(keycloakAdminPassword string) {
	if !vpnClusterEnabled() || !managedKeycloakEnabled() {
		return
	}
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Keycloak == nil || cluster.Keycloak.DNS == "" {
		return
	}

	lines := []string{
		"",
		"  Sign in to Keycloak admin and create a user, then use that user to",
		"  sign in to the NetBird dashboard below (NetBird login is Keycloak SSO).",
		"",
	}
	lines = append(lines,
		keycloakAdminLoginLines(cluster.Keycloak.DNS, cluster.Keycloak.Realm, keycloakAdminPassword)...,
	)
	lines = append(lines, "")

	printNextStepsBox("Create your Keycloak login first", lines)
}

// netbirdDashboardHost returns the NetBird dashboard hostname for the
// instructions panel: cluster.netbird.dns on VPN clusters, the
// netbird.<base> Keycloak-DNS convention on workload clusters, and a
// placeholder when neither derives.
func netbirdDashboardHost() string {
	cluster := config.ParsedGeneralConfig.Cluster

	if cluster.NetBird != nil && cluster.NetBird.DNS != "" {
		return cluster.NetBird.DNS
	}

	if cluster.Keycloak != nil {
		if host := expectedNetBirdHost(cluster.Keycloak.DNS); host != "" {
			return host
		}
	}

	return "<your-netbird-mgmt-dns>"
}

// netBirdOperatorSecretExists returns true when the Secret is present
// and carries a non-empty NB_API_KEY. An empty value would let the
// operator pod schedule but fail at runtime — same outcome as a
// missing Secret, so we treat it the same.
func netBirdOperatorSecretExists(
	ctx context.Context,
	c client.Client,
) (bool, error) {
	secret := &coreV1.Secret{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: netBirdOperatorSecretNamespace,
		Name:      netBirdOperatorSecretName,
	}, secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(secret.Data[netBirdOperatorSecretKey]) > 0, nil
}

// waitForNetBirdOperatorSecret polls every 10s for the Secret to
// appear with a non-empty NB_API_KEY value. Bounded at 30 minutes to
// keep CI / unattended runs from hanging forever — the operator can
// always restart kubeaid-cli later and we'll pick up where we left
// off (the Secret check at the top is idempotent).
func waitForNetBirdOperatorSecret(
	ctx context.Context,
	c client.Client,
) error {
	const (
		interval = 10 * time.Second
		timeout  = 30 * time.Minute
	)
	deadline := time.Now().Add(timeout)

	for {
		exists, err := netBirdOperatorSecretExists(ctx, c)
		if err != nil {
			slog.WarnContext(ctx, "Failed checking NetBird operator Secret, will retry",
				slog.Any("err", err))
		}
		if exists {
			slog.InfoContext(ctx, "NetBird operator API-key Secret detected, continuing bootstrap")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timed out after %s waiting for Secret %s/%s — see instructions printed above",
				timeout, netBirdOperatorSecretNamespace, netBirdOperatorSecretName,
			)
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.New(
				"bootstrap aborted before NetBird operator Secret was created; "+
					"re-run kubeaid-cli once you've created the Secret to continue"))
		case <-time.After(interval):
		}
	}
}

// printNetBirdOperatorInstructions renders the rounded-border panel
// telling the operator how to mint a service-user PAT and persist it
// as the netbird-mgmt-api-key Secret. Same renderer as the
// post-bootstrap next-steps panel so the visual style stays
// consistent across the bootstrap UX.
func printNetBirdOperatorInstructions(netbirdDNS string) {
	dashboardURL := "https://" + netbirdDNS + "/"

	// The network router references an existing NetBird DNS zone and the
	// traefik-internal networkResource references a group — both are
	// dashboard-side (kubeaid-cli only points at them by name), so the
	// operator has to create them here too.
	cluster := config.ParsedGeneralConfig.Cluster
	meshDNSZone := ""
	if cluster.NetBird != nil {
		meshDNSZone = cluster.NetBird.DNSZone
	}
	internalGroup := "k8s-" + cluster.Name

	lines := []string{
		"",
		"  The netbird-operator on this cluster needs a NetBird API token",
		"  (service-user PAT) to start. Without it the operator pod stays in",
		"  CreateContainerConfigError and can't serve its admission webhook.",
		"",
		"  Steps in the NetBird Dashboard:",
		"",
		"    1. Sign in:    " + dashboardURL,
		"    2. Sidebar  →  Team  →  Service Users  →  + Create Service User",
		"          Name:  k8s-operator",
		"          Role:  Admin",
		"    3. From the new user's row  →  ⋮  →  Tokens  →  + Generate Token",
		"          Name:        kubeaid-operator",
		"          Expiration:  pick the longest available (rotation note below)",
		"          Copy the token (shown only once).",
	}

	// Only shown when a mesh DNS zone is set — that's when kubeaid-cli renders
	// the network router + traefik-internal networkResource that need them.
	if meshDNSZone != "" {
		lines = append(lines,
			"    4. Sidebar  →  Networks  →  DNS zones  →  + create a DNS zone:",
			"          Domain:  "+meshDNSZone,
			"          (the network router publishes exposed Services under it)",
			"    5. Sidebar  →  Team  →  Groups  →  + create a group:",
			"          Name:  "+internalGroup,
			"          (the internal Traefik is exposed to this group — add a",
			"           NetBird policy granting your peers access to it)",
		)
	}

	lines = append(lines,
		"",
		"  Then persist the token — pick one:",
		"",
		"  1. secrets.yaml (preferred — kubeaid seals + commits it, survives re-creation):",
		"       netbird:",
		"         apiKey: <paste-token-here>",
		"     then re-run kubeaid-cli.",
		"",
		"  2. Sealed Secret (git-safe; commit the output for durability):",
		"",
	)
	lines = append(lines, sealedSecretCommandLines("       ")...)
	lines = append(lines,
		"",
		"  Bootstrap will resume automatically once the Secret exists (polls",
		"  every 10s, gives up after 30 minutes). Ctrl+C to abort if you'd",
		"  rather handle this later — re-running kubeaid-cli picks up here.",
		"",
		"  Rotation: NetBird user PATs cap at 180 days. Service-user PATs",
		"  may allow longer; pick the maximum the UI offers. Plan a calendar",
		"  reminder until upstream supports no-expiry service-user tokens.",
		"",
	)

	printNextStepsBox("NetBird operator API key required", lines)
}
