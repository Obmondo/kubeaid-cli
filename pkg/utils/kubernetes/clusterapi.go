// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/duration"
	cloudProviderAPI "k8s.io/cloud-provider/api"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

const (
	defaultCapiClusterNamespace = "capi-cluster"

	// statusReady / statusUnknown are the operator-facing Node STATUS
	// strings, also reused for the Cluster status-detail column.
	statusReady   = "Ready"
	statusUnknown = "Unknown"

	// emDash is the placeholder rendered in a status-table cell when
	// there's no value to show.
	emDash = "—"

	// statusMaxWidth caps the Status column's char count to keep the
	// lipgloss table from overflowing on long CAPH error messages.
	// 120 fits a typical 140-column terminal once the Resource and
	// Phase columns are also rendered, and leaves enough room for the
	// "<state> + first error fragment" content operators actually
	// need to spot. The full untruncated message is still available
	// via slog's persisted log file.
	statusMaxWidth = 120
)

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

// GetCapiClusterNamespace returns the namespace where the cloud-credentials
// Secret lives and where CAPI watches Cluster / Machine resources.
//
// Always "capi-cluster". Earlier revisions appended the Obmondo customer ID
// (e.g. capi-cluster-enableit) under the assumption that one management
// cluster would host multiple customers; in practice the management cluster
// is throw-away and each workload cluster's CAPI is single-tenant, so the
// suffix added complexity without isolating anything real. Existing clusters
// running CAPI under the old per-customer namespace need a one-time
// migration (kubectl get + replace + apply) or to opt back into the old
// behaviour via the chart's global.capiClusterNamespace values key.
func GetCapiClusterNamespace() string {
	return defaultCapiClusterNamespace
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
// nil, the caller emits its own "✓ Main cluster reachable (CP + worker
// Machine joined)" substep.
//
// Predicate: Cluster.Status.Phase=Provisioned + ReadyCondition=True
// AND at least one control-plane Machine in Phase=Running with a
// Node ref AND (when worker Machines exist at all) at least one
// worker Machine in Phase=Running with a Node ref.
//
// Why all three:
//   - Cluster Provisioned alone fires as soon as the InfrastructureCluster
//     (HetznerCluster) reports ready (control-plane endpoint set). That can
//     be 10+ min before kubeadm-init finishes on the first CP node; downstream
//     steps then i/o-timeout against an unreachable API.
//   - One CP Machine Running with a Node proves kubeadm-init finished and the
//     API server is genuinely reachable.
//   - One worker Machine Running with a Node proves a schedulable, untainted
//     node exists for the workload-cluster chart installs (Cilium, CCM,
//     kube-prometheus, …). The worker check is skipped for single-CP
//     clusters with no worker MachineDeployments declared — those clusters
//     accept CP-only scheduling on purpose.
//
// The stricter all-Machines-Running gate (every CP + every worker) runs
// separately in WaitForAllMachinesRunning, just before clusterctl move.
func WaitForMainClusterToBeProvisioned(ctx context.Context, managementClusterClient client.Client) error {
	return waitForCAPIStableState(ctx,
		"Waiting for main cluster (CP + worker Machine to join)",
		"main cluster did not reach Provisioned with one CP + one worker Machine Running",
		func(c context.Context) ([]capiStatusRow, bool, error) {
			return summarizeCAPIStatus(c, managementClusterClient)
		},
		nil,
	)
}

// WaitForAllMachinesRunning blocks until every Machine in the
// capi-cluster namespace has reached Phase=Running with status.nodeRef
// populated. This is `clusterctl move`'s headline pre-condition: it
// refuses to start the move while any Machine is still bringing up its
// Node. Earlier in the bootstrap, WaitForMainClusterToBeProvisioned has
// already cleared the *initial* provisioning — but SetupCluster runs
// long-lived ArgoCD syncs after that, any of which can flip the
// KubeadmControlPlane spec (chart upgrade between bootstrap attempts,
// values change, etc.) and trigger a control-plane rolling update.
// That rollout leaves us back at "one of N Machines is mid-provision"
// by the time pivotCluster fires, which is exactly when clusterctl
// move would error out. This wait makes the operation idempotent on
// rolling-update collisions.
//
// While waiting it shows the same live Machine-status table as
// WaitForMainClusterToBeProvisioned. On success it swaps that table for
// a `kubectl get nodes`-style table built from mainClusterClient — the
// live table was the during-the-wait view; the Nodes table is the
// persistent pre-pivot audit trail the operator eyeballs before the
// move. Same screen-ownership and timeout semantics. Returns nil only
// when every Machine has a Node registered — empty Machine list counts
// as not-ready (a freshly scaled-to-zero cluster wouldn't be a sensible
// thing to clusterctl-move anyway, and treating it as ready would hide
// a misconfiguration).
func WaitForAllMachinesRunning(ctx context.Context,
	managementClusterClient, mainClusterClient client.Client,
) error {
	return waitForCAPIStableState(ctx,
		"Verifying control plane is ready for pivot",
		"not all Machines reached Phase=Running with status.nodeRef populated",
		func(c context.Context) ([]capiStatusRow, bool, error) {
			return summarizeMachinesForPivot(c, managementClusterClient)
		},
		func() string {
			return renderMainClusterNodesTable(ctx, mainClusterClient)
		},
	)
}

// waitForCAPIStableState is the shared poll loop behind
// WaitForMainClusterToBeProvisioned and WaitForAllMachinesRunning.
// summarize fetches the rows + a ready predicate; the loop renders the
// live table under a one-line spinner header (spinnerLabel + elapsed),
// returns nil on first ready=true, errors after capiWaitTotalTimeout.
// timeoutErrMsgPrefix is the human prefix appended with
// " within <duration>" on the timeout error.
//
// onSuccess, if non-nil, is called once the wait succeeds: its returned
// block replaces the final live-table tick in scrollback (e.g. swapping
// the Machine-status table for a Nodes table). A nil onSuccess — or one
// that returns "" — leaves the last live tick as the audit trail.
//
// Owns the screen (pauses the progress bar) so the cursor-up / clear-
// to-end redraw doesn't fight the spinner's 100 ms re-render. Caller
// must NOT wrap this with bar.InProgress.
func waitForCAPIStableState(ctx context.Context,
	spinnerLabel string,
	timeoutErrMsgPrefix string,
	summarize func(context.Context) ([]capiStatusRow, bool, error),
	onSuccess func() string,
) error {
	bar := progress.FromCtx(ctx)
	bar.Pause()
	defer bar.Resume()

	// One blank line of separation from the preceding output. No prose
	// preamble — the spinner header inside each tick names the wait.
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
	wipePrev := func() {
		if prevBlock != "" {
			fmt.Printf("\033[%dF\033[J", progress.RenderedLineCount(prevBlock))
			prevBlock = ""
		}
	}

	// frame advances once per redraw (~1 Hz) so the spinner glyph in
	// the header line visibly rotates.
	frame := 0
	redraw := func(rows []capiStatusRow) {
		block := buildCAPIWaitBlock(spinnerLabel, frame, time.Since(start), rows)
		if prevBlock != "" {
			fmt.Printf("\033[%dF\033[J", progress.RenderedLineCount(prevBlock))
		}
		fmt.Print(block)
		prevBlock = block
		frame++
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rows, ready, err := summarize(ctx)
		if err != nil {
			// Transient management-cluster API blip. Log to the slog
			// audit trail (not stdout — the live block is there) and
			// keep polling; the next tick will re-render either the
			// recovered state or the same error.
			slog.WarnContext(ctx, "Failed reading CAPI status; will retry next tick", slog.Any("error", err))
		}

		if ready {
			// Swap the in-flight status table for the caller's success
			// block (e.g. a `kubectl get nodes`-style table) — the live
			// table was the during-the-wait view; the success block is
			// the persistent audit trail. If the caller gave no block,
			// or it rendered empty, keep the last in-flight tick.
			if onSuccess != nil {
				if finalBlock := onSuccess(); finalBlock != "" {
					wipePrev()
					fmt.Print(finalBlock)
					return nil
				}
			}
			redraw(rows)
			return nil
		}

		redraw(rows)

		if time.Now().After(deadline) || attempt == maxAttempts {
			return fmt.Errorf("%s within %s", timeoutErrMsgPrefix, capiWaitTotalTimeout)
		}

		// Tick once per second between API polls so the spinner glyph
		// and elapsed counter keep moving — without this the screen
		// freezes for the full poll interval and reads as "stuck". The
		// table content stays the same; only the header line changes.
		nextPoll := time.Now().Add(capiWaitPollInterval)
		for time.Now().Before(nextPoll) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			redraw(rows)
		}
	}
	// Unreachable — the loop returns from inside the deadline check.
	return fmt.Errorf("%s within %s", timeoutErrMsgPrefix, capiWaitTotalTimeout)
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

	// Hetzner bare-metal-specific overlay. Machine.status.conditions are
	// CAPI's lazy copy of CAPH's HBMM status: when HBMM transitions
	// between in-progress sub-states (registering ↔ image-installing
	// after a CAPH backoff/retry) but the Machine-level condition type
	// + status stay the same (InfrastructureReady=False), the Machine
	// controller often doesn't bump the message. The wait table then
	// reads a stale "image-installing" while operators tailing kubectl
	// see "registering" live. Read HBMM directly so the table reflects
	// CAPH's current state without waiting for Machine to catch up. List
	// failure is logged + non-fatal: we fall back to machineStatusDetail.
	var hbmmOverrides map[string]string
	if globals.CloudProviderName == constants.CloudProviderHetzner {
		if msgs, hbmmErr := hbmmStatusMessages(ctx, mgmtClient); hbmmErr == nil {
			hbmmOverrides = msgs
		} else {
			slog.WarnContext(ctx,
				"Failed reading HBMM live statuses; using Machine-derived status",
				slog.Any("error", hbmmErr))
		}
	}

	rows := make([]capiStatusRow, 0, 1+len(machines.Items))

	// clusterReady reflects only the Cluster CR's own predicate
	// (Phase=Provisioned + ReadyCondition=True). It's necessary but
	// not sufficient — see the gates below.
	clusterReady := false

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
					condition.Status == coreV1.ConditionTrue {
					clusterReady = true
					break
				}
			}
		}
	}

	// Track how many CP / worker Machines exist at all (any phase) and
	// how many of each have reached Phase=Running + NodeRef. The wait
	// must not declare success until at least one CP AND at least one
	// worker have joined the cluster — the Cluster CR's "Provisioned"
	// phase fires as soon as HetznerCluster.status.ready flips True
	// (control-plane endpoint configured), which can be 10+ minutes
	// before the first node actually registers. Downstream steps
	// (SaveProvisionedClusterKubeconfig → CreateKubernetesClient →
	// HelmInstall Cilium → ...) then i/o-timeout against an unreachable
	// API or schedule against zero untainted nodes. Gating here on
	// real Machine progress lets kubeaid-cli sit through the long
	// provisioning window with a single accurate spinner rather than
	// failing-then-retrying downstream.
	var cpTotal, cpRunning, workerTotal, workerRunning int

	for _, m := range machines.Items {
		detail := machineStatusDetail(&m)
		if m.Spec.InfrastructureRef.Kind == "HetznerBareMetalMachine" {
			if override, ok := hbmmOverrides[m.Spec.InfrastructureRef.Name]; ok && override != "" {
				detail = override
			}
		}

		row := capiStatusRow{
			Resource: "Machine/" + m.Name,
			Phase:    m.Status.Phase,
			Status:   detail,
			Failed:   isMachineFailed(&m),
		}
		rows = append(rows, row)

		// Select the right pair of counters once per Machine. Default
		// to the worker accumulators; the CP label flips them.
		total, running := &workerTotal, &workerRunning
		if isControlPlaneMachine(&m) {
			total, running = &cpTotal, &cpRunning
		}
		*total++

		if m.Status.Phase != string(clusterAPIV1Beta1.MachinePhaseRunning) || m.Status.NodeRef == nil {
			continue
		}
		*running++
	}

	// Final predicate: Cluster reached Provisioned, AND at least one CP
	// Machine is Running with a Node, AND (when worker Machines are
	// expected — i.e. any non-CP Machine exists) at least one worker
	// Machine is Running with a Node. The worker check is skipped when
	// cpTotal>0 and workerTotal==0 so a single-CP cluster (no worker
	// MachineDeployments declared) doesn't deadlock here.
	ready := clusterReady && cpRunning > 0 && (workerTotal == 0 || workerRunning > 0)

	return rows, ready, firstErr
}

// isControlPlaneMachine reports whether the Machine is a control-plane
// Machine, distinguished by the `cluster.x-k8s.io/control-plane` label
// that CAPI's KubeadmControlPlane controller stamps onto every CP
// Machine. Workers (owned by MachineSets under MachineDeployments)
// don't carry the label. Value-agnostic — CAPI sets the label with an
// empty value on most versions, so we check key presence only.
func isControlPlaneMachine(m *clusterAPIV1Beta1.Machine) bool {
	if m.Labels == nil {
		return false
	}
	_, ok := m.Labels[clusterAPIV1Beta1.MachineControlPlaneLabel]
	return ok
}

// hbmmStatusMessages reads HetznerBareMetalMachine CRs in the capi-cluster
// namespace and returns a map of HBMM name -> live status message.
//
// Used by summarizeCAPIStatus to overlay the Machine row's status when
// CAPI's lagged copy of CAPH status is staler than what CAPH itself sees.
// See the caller for the full rationale.
//
// Empty map (no error) when no HBMMs exist yet — typical very early in
// bootstrap before kubeadm-control-plane has rolled the first Machine.
func hbmmStatusMessages(ctx context.Context, mgmtClient client.Client) (map[string]string, error) {
	hbmms := &caphV1Beta1.HetznerBareMetalMachineList{}
	if err := mgmtClient.List(ctx, hbmms, client.InNamespace(GetCapiClusterNamespace())); err != nil {
		return nil, err
	}

	out := make(map[string]string, len(hbmms.Items))
	for i := range hbmms.Items {
		hbmm := &hbmms.Items[i]
		if msg := hbmmLiveMessage(hbmm); msg != "" {
			out[hbmm.Name] = msg
		}
	}
	return out, nil
}

// hbmmLiveMessage returns the most informative single-line message from
// an HBMM's status:
//   - terminal failure first (status.failureMessage),
//   - then the Ready condition's Message (where CAPH writes the live
//     "host (<id>) is still provisioning - state '<X>'" string),
//   - then the Ready condition's Reason as a token fallback when Message
//     is empty (typical right after the condition is first seeded).
//
// Returns "" when nothing's failing, no Ready condition exists yet, or
// the Ready condition is True — in which case the caller should fall back
// to the generic Machine-derived status (or no override at all when the
// Machine itself is Running).
func hbmmLiveMessage(hbmm *caphV1Beta1.HetznerBareMetalMachine) string {
	if hbmm.Status.FailureMessage != nil && *hbmm.Status.FailureMessage != "" {
		return firstNonEmptyLine(*hbmm.Status.FailureMessage)
	}
	for _, c := range hbmm.Status.Conditions {
		if c.Type != clusterAPIV1Beta1.ReadyCondition {
			continue
		}
		if c.Status == coreV1.ConditionTrue {
			return ""
		}
		if c.Message != "" {
			return firstNonEmptyLine(c.Message)
		}
		return string(c.Reason)
	}
	return ""
}

// summarizeMachinesForPivot lists Machines in the capi-cluster
// namespace and returns one capiStatusRow per Machine plus a `ready`
// flag set when every Machine has Phase=Running AND a populated
// status.nodeRef. That predicate matches clusterctl move's internal
// `cannot start the move operation while ... is still provisioning the
// node` check (clusterctl walks every Machine's status.nodeRef before
// pivoting) so a true here means the pivot should clear pre-conditions.
//
// Empty Machine list returns ready=false — clusterctl-moving an empty
// namespace would either be a misconfiguration or a no-op, neither of
// which is a useful state to short-circuit on.
//
// Row shape mirrors summarizeCAPIStatus so the same renderCAPIStatusTable
// produces a consistent UX across both wait phases.
func summarizeMachinesForPivot(ctx context.Context, mgmtClient client.Client) ([]capiStatusRow, bool, error) {
	machines := &clusterAPIV1Beta1.MachineList{}
	if err := mgmtClient.List(ctx, machines, client.InNamespace(GetCapiClusterNamespace())); err != nil {
		return nil, false, err
	}

	rows := make([]capiStatusRow, 0, len(machines.Items))
	for _, m := range machines.Items {
		rows = append(rows, capiStatusRow{
			Resource: "Machine/" + m.Name,
			Phase:    m.Status.Phase,
			Status:   machineStatusDetail(&m),
			Failed:   isMachineFailed(&m),
		})
	}

	if len(machines.Items) == 0 {
		return rows, false, nil
	}

	allReady := true
	for _, m := range machines.Items {
		if m.Status.Phase != string(clusterAPIV1Beta1.MachinePhaseRunning) ||
			m.Status.NodeRef == nil {
			allReady = false
			break
		}
	}

	return rows, allReady, nil
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
			condition.Status == coreV1.ConditionTrue {
			return statusReady
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
		if c.Status == coreV1.ConditionTrue {
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
	return emDash
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

// capiWaitSpinnerFrames is the braille spinner cycled in each CAPI-wait
// tick's header line — one frame advance per redraw (~1 Hz).
var capiWaitSpinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// buildCAPIWaitBlock returns one tick's render — a spinner header line
// (`<glyph> <label>  [<elapsed>]`) plus the lipgloss status table — as a
// single string ending in a newline. Returning a string (not printing
// directly) lets the caller count lines for the cursor-up wipe on the
// next tick.
func buildCAPIWaitBlock(spinnerLabel string, frame int, elapsed time.Duration, rows []capiStatusRow) string {
	glyph := capiWaitSpinnerFrames[frame%len(capiWaitSpinnerFrames)]
	header := fmt.Sprintf("%c %s  [%s]\n", glyph, spinnerLabel, elapsed.Round(time.Second))
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
		tableRows = append(tableRows, []string{emDash, "querying…", emDash})
	}
	for _, r := range rows {
		phase := r.Phase
		if phase == "" {
			phase = emDash
		}
		status := truncateForTable(r.Status, statusMaxWidth)
		if status == "" {
			status = emDash
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

// truncateForTable returns s clipped to max runes with a trailing ellipsis
// when it overruns. Rune-based (not byte-based) so a multi-byte tail
// character isn't sliced mid-codepoint — the column messages routinely
// contain em-dashes / curly quotes from CAPH error strings. Returns s
// unchanged when it fits; returns s unchanged when max <= 0.
func truncateForTable(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	keep := max - 1
	if keep < 1 {
		keep = 1
	}
	return string(runes[:keep]) + "…"
}

// renderMainClusterNodesTable returns a `kubectl get nodes -o wide`-style
// table for the main cluster as a string ending in a newline, or "" if
// the Nodes can't be listed (the error is logged; the caller then keeps
// whatever it already had on screen). Used as WaitForAllMachinesRunning's
// success block: once every Machine has a Node, this view is the
// persistent pre-pivot audit trail — the live Machine-status table was
// only useful while the wait was in flight.
//
// Columns mirror kubectl's -o wide output trimmed to the seven most
// useful at pivot time: NAME, STATUS, ROLES, AGE, VERSION,
// INTERNAL-IP, EXTERNAL-IP. OS-IMAGE / KERNEL-VERSION /
// CONTAINER-RUNTIME are omitted; they're identical across every node
// by construction (CAPI's KubeadmConfig preKubeadmCommands pin them)
// and only inflate the row width.
func renderMainClusterNodesTable(ctx context.Context, mainClusterClient client.Client) string {
	nodes := &coreV1.NodeList{}
	if err := mainClusterClient.List(ctx, nodes); err != nil {
		slog.WarnContext(ctx,
			"Failed listing Nodes for the pre-pivot table; keeping the Machine-status table instead",
			slog.Any("error", err),
		)
		return ""
	}

	headers := []string{"NAME", "STATUS", "ROLES", "AGE", "VERSION", "INTERNAL-IP", "EXTERNAL-IP"}
	tableRows := make([][]string, 0, len(nodes.Items))
	now := time.Now()
	for _, node := range nodes.Items {
		tableRows = append(tableRows, []string{
			node.Name,
			nodeReadyStatus(&node),
			nodeRoles(&node),
			duration.HumanDuration(now.Sub(node.CreationTimestamp.Time)),
			node.Status.NodeInfo.KubeletVersion,
			nodeAddress(&node, coreV1.NodeInternalIP),
			nodeAddress(&node, coreV1.NodeExternalIP),
		})
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	notReadyStyle := cellStyle.Foreground(lipgloss.Color("203")) // red
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers(headers...).
		Rows(tableRows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			// Paint NotReady / Unknown rows red so operators don't miss
			// a degraded node before the pivot.
			if row >= 0 && row < len(tableRows) && tableRows[row][1] != statusReady {
				return notReadyStyle
			}
			return cellStyle
		})

	return t.Render() + "\n"
}

// nodeReadyStatus reads the Node's Ready condition and returns the
// "STATUS" column kubectl shows: Ready / NotReady / Unknown.
func nodeReadyStatus(n *coreV1.Node) string {
	for _, c := range n.Status.Conditions {
		if c.Type != coreV1.NodeReady {
			continue
		}
		switch c.Status {
		case coreV1.ConditionTrue:
			return statusReady
		case coreV1.ConditionFalse:
			return "NotReady"
		case coreV1.ConditionUnknown:
			return statusUnknown
		}
		return statusUnknown
	}
	return statusUnknown
}

// nodeRoles returns the comma-joined sorted list of role suffixes
// (everything after "node-role.kubernetes.io/") so a control-plane
// node renders as "control-plane" and a vanilla worker renders as
// "<none>" — same shape kubectl produces.
func nodeRoles(n *coreV1.Node) string {
	const prefix = "node-role.kubernetes.io/"
	var roles []string
	for label := range n.Labels {
		if strings.HasPrefix(label, prefix) {
			roles = append(roles, strings.TrimPrefix(label, prefix))
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

// nodeAddress returns the first address of the given type, or
// "<none>" when the node has none (typical for INTERNAL-IP on
// public-IP-only nodes, or EXTERNAL-IP on private-network nodes).
func nodeAddress(n *coreV1.Node, t coreV1.NodeAddressType) string {
	for _, a := range n.Status.Addresses {
		if a.Type == t {
			return a.Address
		}
	}
	return "<none>"
}

// waitForCPNodesNetworkingTimeout caps the WaitForCPNodesNetworkingReady
// wait. CNI installation on a fresh control-plane Node typically reports
// back within 60-120 s on Hetzner / AWS / Azure; 10 min is a generous
// safety net before we surface a clear error instead of hanging
// indefinitely. Test seam — overridden in unit tests for sub-second
// timeouts.
var waitForCPNodesNetworkingTimeout = 10 * time.Minute

// WaitForCPNodesNetworkingReady blocks until every control-plane Node
// in the main cluster reports BOTH Ready=True AND
// NetworkUnavailable=False, ctx is cancelled, or
// waitForCPNodesNetworkingTimeout passes.
//
// CAPI's Cluster.Phase=Provisioned + ReadyCondition=True (the predicate
// WaitForMainClusterToBeProvisioned waits on above) flips True the
// moment the static control-plane pods
// (apiserver/etcd/controller-manager/scheduler) respond on the
// cluster's apiserver endpoint. It has no signal about whether the CNI
// is installed and a Node can actually schedule pods. Historically
// that gap has masked cilium postKubeadm install failures (e.g. the
// `helm install cilium --atomic --wait` rolling back when hubble
// Deployments stay Pending behind the kubeadm
// control-plane:NoSchedule taint on a single-node bootstrap) —
// kubeaid-cli marched past WaitForMainClusterToBeProvisioned into
// SetupCluster, then SealedSecrets / ArgoCD App sync surfaced as the
// failing layer with workload pods stuck ContainerCreating
// indefinitely.
//
// NetworkUnavailable=False is the standard kubelet/CNI predicate
// Kubernetes itself uses to gate workload scheduling on a Node
// (kubernetes/kubernetes#k8s.io/api/core/v1.NodeNetworkUnavailable),
// so this check is CNI-agnostic (cilium, calico, weave, anything) and
// aligned with what the scheduler would care about anyway. Ready=True
// is the broader kubelet "I can run pods" predicate; we require both
// because either alone has known false positives during cloud-provider
// init.
func WaitForCPNodesNetworkingReady(ctx context.Context, kubeClient client.Client) error {
	ctx, cancel := context.WithTimeout(ctx, waitForCPNodesNetworkingTimeout)
	defer cancel()

	var lastReasons []string
	for {
		nodes := &coreV1.NodeList{}
		if err := kubeClient.List(ctx, nodes); err != nil {
			slog.WarnContext(ctx,
				"Failed listing Nodes for networking-ready check; will retry next tick",
				slog.Any("error", err),
			)
		} else {
			ready, reasons := summarizeCPNodesNetworking(nodes)
			if ready {
				slog.InfoContext(ctx,
					"Every control-plane Node reports Ready=True and NetworkUnavailable=False",
				)
				return nil
			}
			lastReasons = reasons
			slog.InfoContext(ctx,
				"Waiting for control-plane Nodes' networking to be ready",
				slog.Any("not_ready", reasons),
			)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"timed out waiting for control-plane Nodes to report Ready=True and "+
					"NetworkUnavailable=False within %s — suggests the CNI install in "+
					"postKubeadm failed (or rolled back). Last-seen reasons: %v. Run "+
					"`kubectl get pods -n cilium` and `kubectl get nodes -o wide` "+
					"against the provisioned cluster's kubeconfig to investigate",
				waitForCPNodesNetworkingTimeout, lastReasons,
			)
		case <-time.After(waitForProvisioningPollInterval):
		}
	}
}

// summarizeCPNodesNetworking returns whether every control-plane Node
// in nodes is Ready=True AND NetworkUnavailable=False, plus a slice of
// human-readable reasons for each not-ready Node (used in both slog
// tracing during the wait and in the final timeout error message).
//
// Empty CP set returns ready=false with a "no control-plane Nodes
// found" reason — we should never reach this wait without any CP Nodes
// registered (CAPI's Cluster Ready predicate that gates the call site
// requires the control plane to have initialized), so the empty case
// indicates something earlier broke and we should NOT silently pass.
func summarizeCPNodesNetworking(nodes *coreV1.NodeList) (bool, []string) {
	var reasons []string
	cpCount := 0
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if !isControlPlaneNode(node) {
			continue
		}
		cpCount++

		readyOK := false
		// NodeNetworkUnavailable absent from Conditions = network IS
		// available. kubelet only sets the condition when there's a
		// problem (typically: route controller hasn't initialized yet,
		// or CNI hasn't reported Ready). So the default-true here is
		// the correct interpretation.
		netOK := true
		for _, c := range node.Status.Conditions {
			//nolint:exhaustive // only NodeReady and NodeNetworkUnavailable
			// gate scheduling here; the other Node condition types
			// (MemoryPressure, DiskPressure, PIDPressure) are intentionally ignored.
			switch c.Type {
			case coreV1.NodeReady:
				readyOK = c.Status == coreV1.ConditionTrue
			case coreV1.NodeNetworkUnavailable:
				netOK = c.Status == coreV1.ConditionFalse
			}
		}
		if !readyOK {
			reasons = append(reasons, fmt.Sprintf("%s: Ready!=True", node.Name))
		}
		if !netOK {
			reasons = append(reasons, fmt.Sprintf("%s: NetworkUnavailable=True", node.Name))
		}
	}
	if cpCount == 0 {
		return false, []string{"no control-plane Nodes found"}
	}
	return len(reasons) == 0, reasons
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
