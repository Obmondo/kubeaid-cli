// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	coreV1 "k8s.io/api/core/v1"
	policyV1 "k8s.io/api/policy/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

// KubeOne drains each node during an upgrade. On a single-node cluster no evicted pod can
// reschedule (the only node is cordoned), so ANY PodDisruptionBudget selecting running pods
// deadlocks the drain - even 'maxUnavailable: 1' : the first eviction consumes the budget,
// the replacement stays Pending, and every further eviction is forbidden, forever.

type removedPDB struct {
	pdb         policyV1.PodDisruptionBudget
	argoCDOwned bool
}

// neutralizeSingleNodePDBs guards 'kubeone apply' against PDB-deadlocked drains on
// single-node clusters : it lists every pod-selecting PodDisruptionBudget, shows them to the
// operator and asks for consent. On approval it deletes them, keeps re-deleting them in the
// background while the apply runs (ArgoCD self-heal recreates the ones it manages within
// seconds, which would re-wedge the drain), and hands the removed PDBs back for restorePDBs.
// On decline - or when there's no TTY to ask on - the upgrade aborts with the exact kubectl
// commands to handle them by hand.
//
// No-op on multi-node clusters, and when the cluster is unreachable (the resume-from-failure
// path : 'kubeone apply' rebuilds its own view over SSH anyway).
func neutralizeSingleNodePDBs(ctx context.Context) ([]removedPDB, func()) {
	bar := progress.FromCtx(ctx)

	clusterClient, err := getMainClusterClient(ctx)
	if err != nil {
		slog.WarnContext(
			ctx,
			"Skipping the PodDisruptionBudget preflight - the main cluster isn't reachable",
			slog.Any("err", err),
		)
		return nil, func() {}
	}

	nodes := &coreV1.NodeList{}
	if err := clusterClient.List(ctx, nodes); err != nil || (len(nodes.Items) != 1) {
		return nil, func() {}
	}

	pdbs := &policyV1.PodDisruptionBudgetList{}
	err = clusterClient.List(ctx, pdbs)
	assert.AssertErrNil(ctx, err, "Failed listing PodDisruptionBudgets")

	blockingPDBs := []policyV1.PodDisruptionBudget{}
	for i := range pdbs.Items {
		// A PDB expecting no pods can't block any eviction.
		if pdbs.Items[i].Status.ExpectedPods == 0 {
			continue
		}
		blockingPDBs = append(blockingPDBs, pdbs.Items[i])
	}
	if len(blockingPDBs) == 0 {
		return nil, func() {}
	}

	for _, pdb := range blockingPDBs {
		bar.Substep(fmt.Sprintf(
			"PodDisruptionBudget %s/%s would deadlock the single-node drain",
			pdb.Namespace, pdb.Name,
		))
	}

	if !confirmPDBRemoval(bar, blockingPDBs) {
		assert.Assert(ctx, false, manualPDBInstructions(blockingPDBs))
	}

	removed := []removedPDB{}
	for i := range blockingPDBs {
		pdb := blockingPDBs[i]

		err := clusterClient.Delete(ctx, &pdb)
		if err != nil && !k8sErrors.IsNotFound(err) {
			assert.AssertErrNil(
				ctx, err, "Failed deleting PodDisruptionBudget",
				slog.String("namespace", pdb.Namespace),
				slog.String("name", pdb.Name),
			)
		}

		removed = append(removed, removedPDB{
			pdb:         sanitizePDBForRestore(pdb),
			argoCDOwned: isArgoCDManaged(&pdb),
		})
		bar.Substep(fmt.Sprintf("Removed PodDisruptionBudget %s/%s", pdb.Namespace, pdb.Name))
	}

	slog.InfoContext(
		ctx,
		"Removed the PodDisruptionBudgets that would deadlock the single-node drain. The ArgoCD managed ones get recreated by ArgoCD after the upgrade; the rest get restored by this run",
		slog.Int("count", len(removed)),
	)

	guardCtx, stopGuard := context.WithCancel(ctx)
	go keepPDBsDeleted(guardCtx, clusterClient, removed)

	return removed, stopGuard
}

// keepPDBsDeleted re-deletes the removed PDBs every 10 seconds until ctx is canceled, so an
// ArgoCD self-heal recreation mid-drain only wedges the (0s-retrying) eviction loop briefly.
func keepPDBsDeleted(ctx context.Context, clusterClient client.Client, removed []removedPDB) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			for _, r := range removed {
				pdb := &policyV1.PodDisruptionBudget{
					ObjectMeta: metaV1.ObjectMeta{
						Namespace: r.pdb.Namespace,
						Name:      r.pdb.Name,
					},
				}
				if err := clusterClient.Delete(ctx, pdb); err == nil {
					slog.InfoContext(
						ctx,
						"Re-deleted PodDisruptionBudget recreated during the drain (ArgoCD self-heal)",
						slog.String("namespace", r.pdb.Namespace),
						slog.String("name", r.pdb.Name),
					)
				}
			}
		}
	}
}

// restorePDBs re-creates the removed PDBs that ArgoCD does NOT manage. Runs after the
// upgraded node is Ready again, so the restored budgets immediately see their pods running.
// The ArgoCD managed ones are deliberately skipped : ArgoCD recreates them on its next sync.
func restorePDBs(ctx context.Context, removed []removedPDB) {
	if len(removed) == 0 {
		return
	}

	bar := progress.FromCtx(ctx)

	clusterClient, err := getMainClusterClient(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing main cluster client")

	for _, r := range removed {
		if r.argoCDOwned {
			continue
		}

		pdb := r.pdb
		err := clusterClient.Create(ctx, &pdb)
		if err != nil && !k8sErrors.IsAlreadyExists(err) {
			assert.AssertErrNil(
				ctx, err, "Failed restoring PodDisruptionBudget",
				slog.String("namespace", pdb.Namespace),
				slog.String("name", pdb.Name),
			)
		}
		bar.Substep(fmt.Sprintf("Restored PodDisruptionBudget %s/%s", pdb.Namespace, pdb.Name))
	}
}

// confirmPDBRemoval asks the operator for consent to remove the listed PDBs for the duration
// of the upgrade (same huh-form shape as the lockdown confirm). Returns false when declined -
// and when the prompt itself can't run (no TTY), so non-interactive runs fail safe into the
// manual instructions instead of silently mutating the cluster.
func confirmPDBRemoval(bar *progress.Bar, blockingPDBs []policyV1.PodDisruptionBudget) bool {
	description := fmt.Sprintf(
		"The cluster has a single node : evicted pods have nowhere to reschedule, so these\n"+
			"PodDisruptionBudgets would deadlock KubeOne's drain :\n\n%s\n\n"+
			"kubeaid-cli can remove them now, keep them removed while the drain runs (ArgoCD\n"+
			"self-heal recreates its own within seconds), and restore them once the upgraded\n"+
			"node is Ready again. The ArgoCD managed ones come back via ArgoCD itself.",
		pdbNameList(blockingPDBs),
	)

	proceed := false

	bar.Pause()
	defer bar.Resume()

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("PodDisruptionBudgets block the single-node drain").
				Description(description),
			huh.NewConfirm().
				Title("Remove them for the duration of the upgrade?").
				Affirmative("Yes, remove and restore afterwards").
				Negative("No, I'll handle them myself").
				Value(&proceed),
		),
	).Run(); err != nil {
		return false
	}

	return proceed
}

// manualPDBInstructions renders the abort message with the exact commands for handling the
// blocking PDBs by hand.
func manualPDBInstructions(blockingPDBs []policyV1.PodDisruptionBudget) string {
	commands := make([]string, 0, len(blockingPDBs))
	for _, pdb := range blockingPDBs {
		commands = append(commands, fmt.Sprintf("  kubectl -n %s delete pdb %s", pdb.Namespace, pdb.Name))
	}

	return fmt.Sprintf(
		`PodDisruptionBudget removal declined - handle them yourself, then rerun 'kubeaid-cli cluster upgrade' :

%s

NOTE : ArgoCD self-heal may recreate the ArgoCD managed ones mid-drain. If the drain wedges on eviction retries, delete the recreated PodDisruptionBudget again from a second terminal.`,
		strings.Join(commands, "\n"),
	)
}

// pdbNameList renders one "  - <namespace>/<name>" line per PDB.
func pdbNameList(pdbs []policyV1.PodDisruptionBudget) string {
	lines := make([]string, 0, len(pdbs))
	for _, pdb := range pdbs {
		lines = append(lines, fmt.Sprintf("  - %s/%s", pdb.Namespace, pdb.Name))
	}
	return strings.Join(lines, "\n")
}

// isArgoCDManaged reports whether ArgoCD tracks the object - via its default label tracking
// (app.kubernetes.io/instance / argocd.argoproj.io/instance) or annotation tracking
// (argocd.argoproj.io/tracking-id).
func isArgoCDManaged(pdb *policyV1.PodDisruptionBudget) bool {
	if _, ok := pdb.Annotations["argocd.argoproj.io/tracking-id"]; ok {
		return true
	}
	if _, ok := pdb.Labels["app.kubernetes.io/instance"]; ok {
		return true
	}
	if _, ok := pdb.Labels["argocd.argoproj.io/instance"]; ok {
		return true
	}
	return false
}

// sanitizePDBForRestore strips the server-populated fields, leaving a copy that can be
// re-created as-is.
func sanitizePDBForRestore(pdb policyV1.PodDisruptionBudget) policyV1.PodDisruptionBudget {
	return policyV1.PodDisruptionBudget{
		ObjectMeta: metaV1.ObjectMeta{
			Namespace:   pdb.Namespace,
			Name:        pdb.Name,
			Labels:      pdb.Labels,
			Annotations: pdb.Annotations,
		},
		Spec: pdb.Spec,
	}
}
