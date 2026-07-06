// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package netbird

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
	"github.com/Obmondo/kubeaid-cli/pkg/utils/ui"
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

// AwaitOperatorToken settles the netbird-mgmt-api-key Secret before
// lockdown: without it the operator's Pod webhook (failurePolicy: Fail) blocks
// every Pod create. When the Secret is missing and stdin is a terminal the
// operator chooses paste-now / wait / defer; without a terminal it polls then
// fails (CI expects the Secret via secrets.yaml).
//
// Returns proceedWithLockdown=false when the operator defers — the caller must
// then skip lockdown and the LB public-interface disable, or they'd lose
// kube-apiserver with no mesh path back. keycloakAdminPassword feeds the
// "create your Keycloak login first" box (NetBird login is Keycloak SSO).
// No-op when the cluster doesn't host the operator.
func AwaitOperatorToken(
	ctx context.Context,
	clusterClient client.Client,
	keycloakAdminPassword string,
) (proceedWithLockdown bool, err error) {
	if !OperatorEnabled() {
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

	// Keycloak box first: the NetBird dashboard login is Keycloak SSO.
	printKeycloakUserSetupForNetBird(keycloakAdminPassword)
	printNetBirdOperatorInstructions(netbirdDashboardHost())

	// No TTY (CI): poll-then-fail — the Secret is expected via secrets.yaml.
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
			// Ctrl+C aborts; a timeout defers instead — leave the provisioned
			// cluster reachable.
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

	case netBirdTokenDefer:
		printNetBirdSetupDeferred()
		return false, nil

	default:
		return false, fmt.Errorf("unknown NetBird token choice: %d", choice)
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

// sealedSecretCommandLines returns the kubeseal command that persists the
// token as a git-safe SealedSecret, indented for a next-steps box.
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

	ui.PrintNextStepsBox("NetBird API key saved (one-off)", lines)
}

// printNetBirdSetupDeferred tells the operator lockdown + the LB disable were
// skipped (no mesh key yet = no way back in) and how to finish later.
func printNetBirdSetupDeferred() {
	ui.PrintNextStepsBox("NetBird setup deferred — cluster left reachable", []string{
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
// first" panel before the NetBird instructions — the dashboard login is
// Keycloak SSO, so a realm user must exist first. No-op without a locally
// managed Keycloak; an empty password falls back to the kubectl-fetch row.
func printKeycloakUserSetupForNetBird(keycloakAdminPassword string) {
	if !config.VPNClusterEnabled() || !config.ManagedKeycloakEnabled() {
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
		ui.KeycloakAdminLoginLines(cluster.Keycloak.DNS, cluster.Keycloak.Realm, keycloakAdminPassword)...,
	)
	lines = append(lines, "")

	ui.PrintNextStepsBox("Create your Keycloak login first", lines)
}

// netbirdDashboardHost returns the NetBird dashboard hostname for the
// instructions panel: cluster.netbird.dns, or a placeholder when unset.
func netbirdDashboardHost() string {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.NetBird != nil && cluster.NetBird.DNS != "" {
		return cluster.NetBird.DNS
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

// waitForNetBirdOperatorSecret polls every 10s (bounded at 30m) for the Secret
// to appear with a non-empty NB_API_KEY; re-running kubeaid-cli resumes here.
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

// printNetBirdOperatorInstructions renders the panel telling the operator how
// to mint a service-user PAT and persist it as the netbird-mgmt-api-key Secret.
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
	//
	// TODO : create the zone + groups via the Mgmt API once the PAT is in hand
	// (the operator only references them, it never creates them) — see
	// docs/TODO.md "Create the mesh DNS zone + groups via the Mgmt API".
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

	ui.PrintNextStepsBox("NetBird operator API key required", lines)
}
