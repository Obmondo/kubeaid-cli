// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudProviderAPI "k8s.io/cloud-provider/api"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

const defaultCapiClusterNamespace = "capi-cluster"

var (
	waitForProvisioningPollInterval = time.Minute
	saveKubeconfigPollInterval      = 2 * time.Second
	outputPathMainClusterKubeconfig = constants.OutputPathMainClusterKubeconfig

	// capiWaitPollInterval is how often the live-status loop re-reads
	// Cluster + Machine + HCloudMachine from the management cluster.
	// 15s is fine-grained enough that operators see HCloud state
	// transitions (starting → initializing → running) between ticks
	// without flooding the management API.
	capiWaitPollInterval = 15 * time.Second

	// capiWaitTotalTimeout caps the live-status wait. Hetzner HCloud
	// provisions usually finish in 5-15 min; 30 min is a generous
	// safety net before we give up. Past that, the wait exits with
	// a clear error so the operator's session doesn't hang
	// indefinitely while the cluster's stuck.
	capiWaitTotalTimeout = 30 * time.Minute
)

// Returns whether we're using Clusterapi or not.
func UsingClusterAPI() (usingClusterAPI bool) {
	switch globals.CloudProviderName {
	case constants.CloudProviderBareMetal, constants.CloudProviderLocal:
		usingClusterAPI = false

	default:
		usingClusterAPI = true
	}
	return usingClusterAPI
}

// Returns the namespace (capi-cluster / capi-cluster-<customer-id>) where the 'cloud-credentials'
// Kubernetes Secret will exist. This Kubernetes Secret will be used by Cluster API to communicate
// with the underlying cloud provider.
func GetCapiClusterNamespace() string {
	capiClusterNamespace := defaultCapiClusterNamespace
	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.CustomerID != "" {
		capiClusterNamespace = fmt.Sprintf(
			defaultCapiClusterNamespace+"-%s",
			config.ParsedGeneralConfig.Obmondo.CustomerID,
		)
	}
	return capiClusterNamespace
}

// capiStatusRow is one row of the live status table — a single
// CAPI resource (Cluster or Machine) and the operator-facing
// snapshot of where it is in the provisioning lifecycle.
type capiStatusRow struct {
	Resource string
	Phase    string
	Status   string

	// Failed marks the row as terminally broken (FailureReason set,
	// or MachinePhase=Failed). The renderer paints these red so the
	// operator can scan a long table at a glance and abort/diagnose
	// instead of sitting through the rest of the timeout.
	Failed bool
}

// WaitForMainClusterToBeProvisioned blocks until the main cluster's
// CAPI Cluster resource reports Phase=Provisioned + ReadyCondition=True,
// ctx is cancelled, or capiWaitTotalTimeout passes. While waiting it
// renders a live lipgloss table showing the Cluster row and one row per
// Machine (with HCloud InstanceState / FailureReason where available),
// re-rendering every capiWaitPollInterval. The last-rendered tick stays
// in scrollback as the audit trail of what state the cluster was in
// when the wait succeeded — operators have already pinged us once asking
// "is it stuck or just slow", so the persisted table answers that for
// future runs.
//
// Owns the screen for its duration: pauses the progress bar so the
// spinner's 100ms re-render goroutine can't \r-overwrite the table rows,
// resumes on exit. Caller should NOT wrap with bar.InProgress — the
// bar's substep stream is below the persisted table. After this returns
// nil, the caller emits its own "✓ Main cluster Machines provisioned"
// substep.
func WaitForMainClusterToBeProvisioned(ctx context.Context, managementClusterClient client.Client) error {
	bar := progress.FromCtx(ctx)
	bar.Pause()
	defer bar.Resume()

	fmt.Println()
	fmt.Println("Waiting for main cluster Machines to come up — Cluster API and the cloud provider are creating servers, installing the OS image, and joining nodes to the control plane.")
	fmt.Println()

	maxAttempts := int(capiWaitTotalTimeout / capiWaitPollInterval)
	start := time.Now()
	deadline := start.Add(capiWaitTotalTimeout)

	// Track the last-rendered block so we can re-measure its visible
	// height (which depends on the *current* terminal width) and
	// surgically wipe exactly that many rows on the next tick.
	// `\033[s` / `\033[u` cursor save/restore turned out unreliable in
	// practice — once the saved position scrolls past the viewport the
	// restore lands somewhere unpredictable, and the previous render
	// leaks into scrollback. Cursor-up + clear-to-end is more
	// deterministic, but the up-count has to account for line
	// wrapping (operator splits a tmux pane mid-run → narrower width →
	// previously-single-line rows now occupy two rows each) — see
	// progress.RenderedLineCount.
	prevBlock := ""

	redraw := func(attempt int, rows []capiStatusRow) {
		block := buildCAPIWaitBlock(attempt, maxAttempts, time.Since(start), rows)
		if prevBlock != "" {
			fmt.Printf("\033[%dF\033[J", progress.RenderedLineCount(prevBlock))
		}
		fmt.Print(block)
		prevBlock = block
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rows, ready, err := summarizeCAPIStatus(ctx, managementClusterClient)
		if err != nil {
			// Transient management-cluster API blip. Log to the slog
			// audit trail (not stdout — the live block is there) and
			// keep polling; the next tick will re-render either the
			// recovered state or the same error.
			slog.WarnContext(ctx, "Failed reading CAPI status; will retry next tick", slog.Any("error", err))
		}

		redraw(attempt, rows)

		if ready {
			return nil
		}

		if time.Now().After(deadline) || attempt == maxAttempts {
			return fmt.Errorf("main cluster did not reach Provisioned+Ready within %s", capiWaitTotalTimeout)
		}

		// Tick once per second between API polls so the elapsed /
		// remaining counter keeps moving — without this the screen
		// freezes for the full 15 s poll interval and reads as
		// "stuck". The table content stays the same; only the
		// per-tick header line gets a fresh timestamp.
		nextPoll := time.Now().Add(capiWaitPollInterval)
		for time.Now().Before(nextPoll) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			redraw(attempt, rows)
		}
	}
	// Unreachable — the loop returns from inside the deadline check.
	return fmt.Errorf("main cluster did not reach Provisioned+Ready within %s", capiWaitTotalTimeout)
}

// summarizeCAPIStatus reads Cluster + MachineList from the management
// cluster and returns one capiStatusRow per resource plus a `ready`
// flag (Cluster Phase=Provisioned + ReadyCondition=True, matching the
// original wait predicate). Pure CAPI — no provider-specific types —
// because the Machine controller already aggregates infrastructure-side
// status into the Machine's own conditions (notably the v1beta2
// `Ready` condition's bullet rollup, which surfaces things like
// `* InfrastructureReady: error during placement (resource_unavailable, ...)`
// without us needing to know whether the underlying provider is CAPH,
// CAPA, CAPZ, or anything else).
//
// Returns rows even on partial failure: if Cluster.Get works but
// MachineList fails (or vice-versa), callers still see whatever
// snapshot we have. The error is reported to the caller so it can
// log; the wait loop treats it as transient and renders what it has.
func summarizeCAPIStatus(ctx context.Context, mgmtClient client.Client) ([]capiStatusRow, bool, error) {
	var firstErr error

	cluster, err := GetClusterResource(ctx, mgmtClient)
	if err != nil {
		firstErr = err
	}

	machines := &clusterAPIV1Beta1.MachineList{}
	if err := mgmtClient.List(ctx, machines, client.InNamespace(GetCapiClusterNamespace())); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}

	rows := make([]capiStatusRow, 0, 1+len(machines.Items))
	ready := false

	if cluster != nil {
		clusterRow := capiStatusRow{
			Resource: "Cluster/" + cluster.Name,
			Phase:    cluster.Status.Phase,
			Status:   clusterStatusDetail(cluster),
		}
		rows = append(rows, clusterRow)

		if cluster.Status.Phase == string(clusterAPIV1Beta1.ClusterPhaseProvisioned) {
			for _, condition := range cluster.Status.Conditions {
				if condition.Type == clusterAPIV1Beta1.ReadyCondition &&
					condition.Status == "True" {
					ready = true
					break
				}
			}
		}
	}

	for _, m := range machines.Items {
		row := capiStatusRow{
			Resource: "Machine/" + m.Name,
			Phase:    m.Status.Phase,
			Status:   machineStatusDetail(&m),
			Failed:   isMachineFailed(&m),
		}
		rows = append(rows, row)
	}

	return rows, ready, firstErr
}

// clusterStatusDetail picks the most useful single-line summary for the
// Cluster row's Status column. CAPI is mid-migration to v1beta2 status:
// the same Cluster object carries both v1beta1 conditions (terse,
// `Reason="ScalingUp"`) and v1beta2 conditions (rich, multi-line
// `Message="* InfrastructureReady: error during placement (...)"`).
// We prefer v1beta2 when it's populated — its messages explicitly
// surface the controller's diagnostic detail — and fall back to v1beta1
// for clusters running against a controller that hasn't started writing
// v1beta2 yet.
func clusterStatusDetail(cluster *clusterAPIV1Beta1.Cluster) string {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == clusterAPIV1Beta1.ReadyCondition &&
			condition.Status == "True" {
			return "Ready"
		}
	}

	if cluster.Status.V1Beta2 != nil {
		if msg := firstFailingV1Beta2Message(cluster.Status.V1Beta2.Conditions); msg != "" {
			return msg
		}
	}

	return firstFailingV1Beta1Message(cluster.Status.Conditions)
}

// isMachineFailed flags a Machine row red even when Phase is still
// "Provisioning". CAPI machines stay in Provisioning while the
// infrastructure controller retries transient errors (Hetzner
// `resource_unavailable`, AWS quota throttling, etc.) — the Phase
// only flips to "Failed" on a terminal condition. Operators want the
// retryable-but-failing case to stand out too, so we also paint red
// when any non-True condition's Reason looks like a failure
// (`ServerCreateFailedReason`, `BootstrapFailed`, `ImagePullError`).
//
// Conditions with Status=Unknown are intentionally skipped — the v1beta2
// schema uses Unknown + reason="InspectionFailed" for "Waiting for
// control plane to be initialized" sub-states, which aren't actual
// failures and would create a wall of false-positive red rows during
// normal in-progress provisioning.
func isMachineFailed(m *clusterAPIV1Beta1.Machine) bool {
	if m.Status.Phase == string(clusterAPIV1Beta1.MachinePhaseFailed) {
		return true
	}
	if m.Status.V1Beta2 != nil {
		for _, c := range m.Status.V1Beta2.Conditions {
			if c.Status == metaV1.ConditionFalse && reasonIndicatesFailure(c.Reason) {
				return true
			}
		}
	}
	for _, c := range m.Status.Conditions {
		if c.Status == "False" && reasonIndicatesFailure(c.Reason) {
			return true
		}
	}
	return false
}

// reasonIndicatesFailure is the cheap substring heuristic used by
// isMachineFailed. CAPI condition Reasons follow camelCase tokens like
// "ServerCreateFailedReason" / "ImagePullError"; substring-matching
// "Failed" or "Error" catches them without us having to enumerate every
// provider's reason vocabulary. Case-insensitive so a future "FAILED"
// or stray lowercase variant doesn't slip through.
func reasonIndicatesFailure(reason string) bool {
	lower := strings.ToLower(reason)
	return strings.Contains(lower, "failed") || strings.Contains(lower, "error")
}

// machineStatusDetail formats the Status column for a Machine row.
// Same v1beta2-then-v1beta1 fallback as clusterStatusDetail so the
// table stays uniform across rows. The Machine controller copies its
// InfrastructureRef status into its own conditions, so reading the
// Machine alone surfaces provider-side errors (e.g., Hetzner's
// placement error) without us needing to look at HCloudMachine /
// AWSMachine / AzureMachine directly.
func machineStatusDetail(m *clusterAPIV1Beta1.Machine) string {
	if m.Status.V1Beta2 != nil {
		if msg := firstFailingV1Beta2Message(m.Status.V1Beta2.Conditions); msg != "" {
			return msg
		}
	}
	return firstFailingV1Beta1Message(m.Status.Conditions)
}

// firstFailingV1Beta2Message returns the most informative single-line
// summary across all non-True v1beta2 conditions. CAPI v1beta2 lists
// every known condition type — many transitional ones have an empty
// Message (e.g. Machine.Available with reason=NotReady, msg=""), but a
// later condition typically holds the rich rollup
// (Machine.Ready: "* InfrastructureReady: error during placement
// (resource_unavailable, ...)"). So we scan the whole list, prefer the
// first condition with a non-empty Message, and only fall back to the
// first non-True Reason if nothing has a Message at all.
//
// Returns "" when nothing's failing so the caller can fall back to the
// v1beta1 path.
func firstFailingV1Beta2Message(conditions []metaV1.Condition) string {
	var fallbackReason, fallbackType string
	for _, c := range conditions {
		if c.Status == metaV1.ConditionTrue {
			continue
		}
		if msg := firstNonEmptyLine(c.Message); msg != "" {
			return msg
		}
		if fallbackReason == "" && fallbackType == "" {
			fallbackReason = c.Reason
			fallbackType = c.Type
		}
	}
	if fallbackReason != "" {
		return fallbackReason
	}
	return fallbackType
}

// firstFailingV1Beta1Message is the v1beta1 fallback path: prefer
// Message (operator-readable, e.g. "Scaling up control plane to 1
// replicas (actual 0)") over Reason (terse machine token, e.g.
// "ScalingUp"). Same skip-empty-Messages-then-fall-back-to-Reason logic
// as the v1beta2 helper for consistency. Returns "—" when nothing's
// failing so the table renders an explicit em-dash rather than empty
// space.
func firstFailingV1Beta1Message(conditions clusterAPIV1Beta1.Conditions) string {
	var fallbackReason string
	var fallbackType clusterAPIV1Beta1.ConditionType
	for _, c := range conditions {
		if c.Status == "True" {
			continue
		}
		if c.Message != "" {
			return firstNonEmptyLine(c.Message)
		}
		if fallbackReason == "" && fallbackType == "" {
			fallbackReason = c.Reason
			fallbackType = c.Type
		}
	}
	if fallbackReason != "" {
		return fallbackReason
	}
	if fallbackType != "" {
		return string(fallbackType)
	}
	return "—"
}

// firstNonEmptyLine returns the first line of s with leading
// whitespace + bullet markers ("* ", "- ") trimmed, so the rollup
// `"* InfrastructureReady: target cluster not ready"` renders as
// `"InfrastructureReady: target cluster not ready"`. v1beta2 messages
// universally use this bullet shape; stripping it reads cleaner in a
// table cell.
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimPrefix(trimmed, "- ")
		return trimmed
	}
	return ""
}

// buildCAPIWaitBlock returns the per-tick render — the header line
// (attempt + elapsed + remaining + Ctrl+C hint) plus the lipgloss
// status table — as a single string ending in a newline. Returning a
// string (not printing directly) lets the caller count lines for the
// cursor-up wipe on the next tick.
func buildCAPIWaitBlock(attempt, maxAttempts int, elapsed time.Duration, rows []capiStatusRow) string {
	remaining := capiWaitTotalTimeout - elapsed
	if remaining < 0 {
		remaining = 0
	}
	header := fmt.Sprintf("Attempt %d/%d  •  %s elapsed  •  %s remaining  •  Ctrl+C to abort\n",
		attempt, maxAttempts,
		elapsed.Round(time.Second), remaining.Round(time.Second),
	)
	return header + renderCAPIStatusTable(rows) + "\n"
}

// renderCAPIStatusTable lays the rows out as a lipgloss table with a
// rounded border (same visual style as the DNS-wait table earlier in
// the bootstrap, so the operator's eye recognizes the pattern). Failed
// rows render in red so they stand out in a long machine list.
func renderCAPIStatusTable(rows []capiStatusRow) string {
	headers := []string{"Resource", "Phase", "Status"}

	tableRows := make([][]string, 0, len(rows))
	if len(rows) == 0 {
		tableRows = append(tableRows, []string{"—", "querying…", "—"})
	}
	for _, r := range rows {
		phase := r.Phase
		if phase == "" {
			phase = "—"
		}
		status := r.Status
		if status == "" {
			status = "—"
		}
		tableRows = append(tableRows, []string{r.Resource, phase, status})
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	failStyle := cellStyle.Foreground(lipgloss.Color("203")) // red

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers(headers...).
		Rows(tableRows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if row >= 0 && row < len(rows) && rows[row].Failed {
				return failStyle
			}
			return cellStyle
		})

	return t.Render()
}

// WaitForMainClusterToBeReady waits for the main cluster to be ready to run
// application workloads. It polls until at least one initialized worker node
// exists or the context is cancelled.
func WaitForMainClusterToBeReady(ctx context.Context, kubeClient client.Client) error {
	for {
		slog.InfoContext(
			ctx,
			"Waiting for the provisioned cluster's Kubernetes API server to be reachable and atleast 1 worker node to be initialized....",
		)

		// List the nodes.
		nodes := &coreV1.NodeList{}
		if err := kubeClient.List(ctx, nodes); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				continue
			}
		}

		initializedWorkerNodeCount := 0
		for _, node := range nodes.Items {
			if isControlPlaneNode(&node) {
				continue
			}

			isInitialized := true
			//
			// Check for existence of taints which indicate that the node is uninitialized.
			for _, taint := range node.Spec.Taints {
				if (taint.Key == cloudProviderAPI.TaintExternalCloudProvider) ||
					(taint.Key == clusterAPIV1Beta1.NodeUninitializedTaint.Key) {
					isInitialized = false
				}
			}

			if isInitialized {
				initializedWorkerNodeCount++
			}
		}

		if initializedWorkerNodeCount > 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitForProvisioningPollInterval):
		}
	}
}

// SaveProvisionedClusterKubeconfig saves kubeconfig of the provisioned cluster locally.
func SaveProvisionedClusterKubeconfig(ctx context.Context, kubeClient client.Client) error {
	secret := &coreV1.Secret{}
	// Seldom, after the cluster has been provisioned, Cluster API takes some time to create the
	// Kubernetes secret containing the kubeconfig.
	for {
		err := kubeClient.Get(ctx,
			types.NamespacedName{
				Name:      fmt.Sprintf("%s-kubeconfig", config.ParsedGeneralConfig.Cluster.Name),
				Namespace: GetCapiClusterNamespace(),
			},
			secret,
		)
		if err == nil {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(saveKubeconfigPollInterval):
		}
	}

	kubeConfig := secret.Data["value"]

	if err := os.WriteFile(outputPathMainClusterKubeconfig, kubeConfig, 0o600); err != nil {
		return fmt.Errorf("failed saving kubeconfig to file: %w", err)
	}

	slog.InfoContext(ctx, "kubeconfig has been saved locally")
	return nil
}

// Looks for and returns the Cluster resource in the given Kubernetes cluster.
func GetClusterResource(ctx context.Context,
	clusterClient client.Client,
) (*clusterAPIV1Beta1.Cluster, error) {
	cluster := &clusterAPIV1Beta1.Cluster{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      config.ParsedGeneralConfig.Cluster.Name,
			Namespace: GetCapiClusterNamespace(),
		},
	}

	if err := GetKubernetesResource(ctx, clusterClient, cluster); err != nil {
		return nil, utils.WrapError("Failed getting Cluster resource", err)
	}
	return cluster, nil
}

// Returns whether the 'clusterctl move' command has already been executed or not.
func IsClusterctlMoveExecuted(ctx context.Context) bool {
	mainClusterClient, err := createKubernetesClientFn(ctx,
		outputPathMainClusterKubeconfig,
	)
	// Main cluster isn't reachable,
	// which means 'clusterctl move' hasn't been executed.
	if err != nil {
		return false
	}

	// If the Cluster resource is found in the main cluster,
	// that means 'clusterctl move' has been executed.
	_, err = GetClusterResource(ctx, mainClusterClient)
	return err == nil
}
