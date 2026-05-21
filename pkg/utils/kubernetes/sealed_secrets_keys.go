// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

const (
	// sealedSecretsActiveKeyLabel is the label sealed-secrets controller
	// uses to mark its active signing/decryption keys. Both the
	// controller's own key generation and the upstream rotation tooling
	// apply this label; matching against it picks up exactly the
	// material we need to copy without including unrelated TLS Secrets
	// that happen to live in the same namespace.
	//
	// See kubeaid/argocd-helm-charts/sealed-secrets/README.md and the
	// upstream bitnami-labs/sealed-secrets docs.
	sealedSecretsActiveKeyLabel = "sealedsecrets.bitnami.com/sealed-secrets-key"
	sealedSecretsActiveKeyValue = "active"

	// sealedSecretsControllerDeploymentName mirrors the Deployment that
	// kubeaid's sealed-secrets chart installs. Used by
	// WaitForSealedSecretsControllerReady — the chart's release name
	// and the Deployment name happen to be the same.
	sealedSecretsControllerDeploymentName = "sealed-secrets-controller"
)

// CopySealedSecretsKeysFromManagement copies every active sealed-secrets
// key Secret from the management cluster's sealed-secrets namespace to
// the main cluster's same namespace. After the copy, the main cluster's
// sealed-secrets controller picks the new key(s) up via its Secret
// watch (typically within a second), adding them to its decryption
// keyring alongside whatever key it generated on first start.
//
// Why we do this on every bootstrap: kubeaid-cli runs kubeseal against
// the *management* controller during the management-cluster phase,
// producing SealedSecret artefacts in kubeaid-config that are encrypted
// with the management controller's key. If the main controller doesn't
// also know that key, those SealedSecrets stay undecryptable forever
// (sealing is non-deterministic, so the per-file kubeaid-sha256 cache
// won't re-seal them on the next run unless the plaintext changed).
//
// Idempotent: re-runs overwrite each copied Secret with the current
// management-side bytes, so a re-bootstrap on top of a partially-
// pivoted cluster converges cleanly. Safe to call when the management
// cluster has zero keys (returns nil, no-op).
//
// Mirrors the DR-restore pattern in pkg/core/setup_cluster.go — same
// shape, different source (live management cluster instead of object-
// storage backup).
func CopySealedSecretsKeysFromManagement(ctx context.Context, mgmt, main client.Client) error {
	var keys coreV1.SecretList
	if err := mgmt.List(ctx, &keys,
		client.InNamespace(constants.NamespaceSealedSecrets),
		client.MatchingLabels{sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue},
	); err != nil {
		return fmt.Errorf("listing sealed-secrets keys on management cluster: %w", err)
	}

	if len(keys.Items) == 0 {
		slog.InfoContext(ctx,
			"No active sealed-secrets keys on management cluster — nothing to copy",
		)
		return nil
	}

	for i := range keys.Items {
		src := &keys.Items[i]
		desired := &coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      src.Name,
				Namespace: constants.NamespaceSealedSecrets,
				Labels: map[string]string{
					sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue,
				},
			},
			Type: src.Type,
			Data: src.Data,
		}

		existing := &coreV1.Secret{}
		err := main.Get(ctx,
			types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name},
			existing,
		)
		switch {
		case k8sAPIErrors.IsNotFound(err):
			if createErr := main.Create(ctx, desired); createErr != nil {
				return fmt.Errorf(
					"creating sealed-secrets key %s/%s on main: %w",
					desired.Namespace, desired.Name, createErr,
				)
			}
			slog.InfoContext(ctx,
				"Copied sealed-secrets key from management to main",
				slog.String("name", desired.Name),
			)
		case err != nil:
			return fmt.Errorf(
				"reading sealed-secrets key %s/%s on main: %w",
				desired.Namespace, desired.Name, err,
			)
		default:
			// Already present — patch data/labels to whatever the
			// management cluster currently has. Preserves any other
			// metadata (annotations, finalizers) the main controller
			// added on top.
			existing.Data = desired.Data
			if existing.Labels == nil {
				existing.Labels = map[string]string{}
			}
			existing.Labels[sealedSecretsActiveKeyLabel] = sealedSecretsActiveKeyValue
			if updateErr := main.Update(ctx, existing); updateErr != nil {
				return fmt.Errorf(
					"updating sealed-secrets key %s/%s on main: %w",
					desired.Namespace, desired.Name, updateErr,
				)
			}
			slog.InfoContext(ctx,
				"Refreshed already-present sealed-secrets key on main",
				slog.String("name", desired.Name),
			)
		}
	}

	return nil
}

// reinstallSealedSecretsFn is the test seam for EnsureSealedSecretsHealthy's
// recovery path. Production wires this to ReinstallSealedSecrets in
// sealed_secrets.go; tests substitute a function variable to assert
// the recovery branch was taken without standing up Helm.
var reinstallSealedSecretsFn = ReinstallSealedSecrets

// healthPollInterval / healthPollTimeoutForTest cap the time
// EnsureSealedSecretsHealthy spends waiting for the controller
// Deployment to settle on each side of the recovery attempt.
//
// 60 seconds is enough to absorb the brief race between Helm's
// install/upgrade returning and the Deployment.Status fields
// catching up — the actual "image pull + container start + readiness
// probe" wait happens inside Helm's own Wait=true loop (10-minute
// budget per pkg/utils/kubernetes/helm.go), not here. Polling for
// minutes on the kubeaid-cli side just duplicates that work.
//
// When the Deployment is genuinely missing (the today-failure mode:
// Helm thinks deployed but operator deleted it), polling at all is
// pointless — only Helm creates Deployments and we just called Helm.
// The 60s budget tolerates the status-update race; anything longer
// is wasted spin.
//
// healthPollTimeoutForTest is a var (not a const) so unit tests can
// shorten the budget to keep test runtime tight.
const healthPollInterval = 2 * time.Second

var healthPollTimeoutForTest = 60 * time.Second

// EnsureSealedSecretsHealthy is the single source of truth for "is
// sealed-secrets actually functional on this cluster?" after we've
// run the install + copied keys.
//
// Two independent checks, two independent recovery actions:
//
//  1. **Key parity** — count of Secrets labelled
//     sealedsecrets.bitnami.com/sealed-secrets-key=active on main must
//     equal the count on mgmt. If short, re-run the copy. Recovery
//     for "the copy didn't actually land" (transient API blip, racing
//     with namespace creation, etc.).
//
//  2. **Controller Deployment health** — Deployment must have
//     AvailableReplicas == desired AND ReadyReplicas == desired AND
//     UnavailableReplicas == 0. If unhealthy after a 5min poll, call
//     ReinstallSealedSecrets (Helm install with Replace=true,
//     bypassing skip-if-deployed). Recovery for "Helm thinks the
//     release is fine but the Deployment was deleted out-of-band"
//     (operator manual recovery, ArgoCD pruning, etc.).
//
// Retry budget = 1 reinstall. If the Deployment is still unhealthy
// afterward, return a rich diagnostic so the operator knows whether
// it's a taint, an image pull, a crashing container, etc. — not just
// "Sealed Secrets controller not Ready".
func EnsureSealedSecretsHealthy(ctx context.Context, mgmt, main client.Client) error {
	// CHECK 1: key parity.
	if err := ensureSealedSecretsKeysReplicated(ctx, mgmt, main); err != nil {
		return fmt.Errorf("ensuring sealed-secrets key parity: %w", err)
	}

	// CHECK 2: controller pod health.
	if err := waitForControllerHealthy(ctx, main, healthPollTimeoutForTest); err == nil {
		return nil // happy path — controller already healthy
	}

	// Reinstall once, then re-check.
	slog.WarnContext(ctx,
		"Sealed-secrets controller not Ready — running ReinstallSealedSecrets (Install.Replace=true) and retrying once",
	)
	if err := reinstallSealedSecretsFn(ctx); err != nil {
		return fmt.Errorf("reinstalling sealed-secrets after unhealthy controller: %w", err)
	}

	if err := waitForControllerHealthy(ctx, main, healthPollTimeoutForTest); err == nil {
		return nil
	}

	// Still unhealthy after reinstall — surface the rich diagnostic.
	diag := diagnoseSealedSecretsController(ctx, main)
	return fmt.Errorf("sealed-secrets controller still not Ready after reinstall:\n%s", diag)
}

// ensureSealedSecretsKeysReplicated does a count check between mgmt
// and main. On mismatch, re-runs the copy and re-checks once. Returns
// an error only if the counts still differ after the recovery copy.
func ensureSealedSecretsKeysReplicated(ctx context.Context, mgmt, main client.Client) error {
	mgmtKeys, err := listActiveSealedSecretsKeys(ctx, mgmt)
	if err != nil {
		return fmt.Errorf("listing sealed-secrets keys on management cluster: %w", err)
	}
	mainKeys, err := listActiveSealedSecretsKeys(ctx, main)
	if err != nil {
		return fmt.Errorf("listing sealed-secrets keys on main cluster: %w", err)
	}

	if len(mainKeys) == len(mgmtKeys) {
		return nil // counts match — copy landed
	}

	if len(mgmtKeys) == 0 {
		// Defensive: mgmt has no keys (operator destroyed sealed-secrets
		// on k3d, or k3d itself was recreated mid-flight). Don't try to
		// "recover" — main is on its own. Operator workflow re-creates
		// k3d on next run, so this case self-heals.
		slog.WarnContext(ctx, "Management cluster has no active sealed-secrets keys — main cluster will generate its own")
		return nil
	}

	slog.WarnContext(ctx,
		"sealed-secrets key count mismatch between mgmt and main — re-running copy",
		slog.Int("mgmt", len(mgmtKeys)),
		slog.Int("main", len(mainKeys)),
	)
	if err := CopySealedSecretsKeysFromManagement(ctx, mgmt, main); err != nil {
		return err
	}

	// Re-check after the copy.
	mainKeys, err = listActiveSealedSecretsKeys(ctx, main)
	if err != nil {
		return fmt.Errorf("re-listing sealed-secrets keys on main after recovery copy: %w", err)
	}
	if len(mainKeys) != len(mgmtKeys) {
		return fmt.Errorf(
			"sealed-secrets key count still mismatched after recovery copy: mgmt=%d main=%d",
			len(mgmtKeys), len(mainKeys),
		)
	}
	slog.InfoContext(ctx, "sealed-secrets key count parity restored",
		slog.Int("keys", len(mainKeys)),
	)
	return nil
}

// listActiveSealedSecretsKeys returns every Secret in the
// sealed-secrets namespace labelled
// sealedsecrets.bitnami.com/sealed-secrets-key=active. The label is
// applied by both the controller's own key generation and by the
// CopySealedSecretsKeysFromManagement copy, so the result accurately
// reflects the controller's decryption keyring.
func listActiveSealedSecretsKeys(ctx context.Context, c client.Client) ([]coreV1.Secret, error) {
	var list coreV1.SecretList
	if err := c.List(ctx, &list,
		client.InNamespace(constants.NamespaceSealedSecrets),
		client.MatchingLabels{sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue},
	); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// errDeploymentMissing is the sentinel waitForControllerHealthy
// returns when the Deployment object doesn't exist at all. Callers
// use it to short-circuit recovery: there's no point polling for a
// Deployment Helm hasn't created. Helm's the only thing that creates
// it, and we already called Helm before the check.
var errDeploymentMissing = fmt.Errorf("sealed-secrets controller Deployment not found")

// waitForControllerHealthy polls the controller Deployment for the
// "fully rolled out and serving" condition: AvailableReplicas equals
// desired, ReadyReplicas equals desired, UnavailableReplicas is zero,
// and ObservedGeneration matches Spec.Generation.
//
// Behaviour:
//   - Deployment exists and matches the condition → returns nil
//   - Deployment exists but status hasn't settled → polls until
//     timeout or condition matches
//   - Deployment doesn't exist at all → returns errDeploymentMissing
//     IMMEDIATELY (no polling). The Deployment is created by Helm;
//     if it isn't there after we ran Helm, polling won't summon it.
//     Callers wanting recovery jump straight to ReinstallSealedSecrets.
//   - Other API error → returned wrapped
func waitForControllerHealthy(ctx context.Context, c client.Client, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, healthPollInterval, timeout, true,
		func(ctx context.Context) (bool, error) {
			dep := &appsV1.Deployment{}
			if err := c.Get(ctx,
				types.NamespacedName{
					Namespace: constants.NamespaceSealedSecrets,
					Name:      sealedSecretsControllerDeploymentName,
				},
				dep,
			); err != nil {
				if k8sAPIErrors.IsNotFound(err) {
					// No point polling — Helm creates Deployments,
					// kubeaid-cli is the only thing calling Helm in
					// this flow, and we already called it.
					return false, errDeploymentMissing
				}
				return false, fmt.Errorf("reading sealed-secrets controller Deployment: %w", err)
			}
			return deploymentFullyAvailable(dep), nil
		},
	)
}

func deploymentFullyAvailable(dep *appsV1.Deployment) bool {
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	if desired == 0 {
		return false
	}
	return dep.Status.ObservedGeneration >= dep.Generation &&
		dep.Status.AvailableReplicas >= desired &&
		dep.Status.ReadyReplicas >= desired &&
		dep.Status.UnavailableReplicas == 0
}

// diagnoseSealedSecretsController collects the highest-signal cluster
// state for an operator-facing error message when the Deployment
// won't become healthy: Deployment status conditions, the controller
// pod's phase + container statuses, and any recent Events on the
// Deployment or its pods.
//
// Best-effort: every lookup error is folded into the returned string
// rather than propagated, because the caller is already returning an
// error and we want to surface as much as possible alongside the
// "not Ready" message.
func diagnoseSealedSecretsController(ctx context.Context, c client.Client) string {
	var lines []string
	push := func(s string) { lines = append(lines, "  "+s) }

	// Deployment status.
	dep := &appsV1.Deployment{}
	depErr := c.Get(ctx,
		types.NamespacedName{
			Namespace: constants.NamespaceSealedSecrets,
			Name:      sealedSecretsControllerDeploymentName,
		},
		dep,
	)
	switch {
	case k8sAPIErrors.IsNotFound(depErr):
		push("Deployment sealed-secrets/sealed-secrets-controller — NOT FOUND")
	case depErr != nil:
		push(fmt.Sprintf("Deployment lookup error: %v", depErr))
	default:
		push(fmt.Sprintf("Deployment generation=%d observedGeneration=%d replicas=%d/desired=%d available=%d ready=%d unavailable=%d",
			dep.Generation, dep.Status.ObservedGeneration,
			dep.Status.Replicas, deploymentDesiredReplicas(dep),
			dep.Status.AvailableReplicas, dep.Status.ReadyReplicas,
			dep.Status.UnavailableReplicas,
		))
		for _, cond := range dep.Status.Conditions {
			push(fmt.Sprintf("Deployment condition %s=%s reason=%s message=%s",
				cond.Type, cond.Status, cond.Reason, truncate(cond.Message)))
		}
	}

	// Pod statuses for that Deployment.
	var pods coreV1.PodList
	if err := c.List(ctx, &pods,
		client.InNamespace(constants.NamespaceSealedSecrets),
		client.MatchingLabels{"name": sealedSecretsControllerDeploymentName},
	); err != nil {
		push(fmt.Sprintf("Pod list error: %v", err))
	} else if len(pods.Items) == 0 {
		push("No pods matched name=sealed-secrets-controller — Deployment may not have produced a ReplicaSet")
	} else {
		for _, line := range describeSealedSecretsPods(pods.Items) {
			push(line)
		}
	}

	// Recent Events on the Deployment + its pods. Best-effort.
	events := &coreV1.EventList{}
	if err := c.List(ctx, events,
		client.InNamespace(constants.NamespaceSealedSecrets),
	); err != nil {
		push(fmt.Sprintf("Event list error: %v", err))
	} else {
		// Filter to Deployment- or Pod-scoped events on our resources.
		relevant := 0
		for i := range events.Items {
			ev := &events.Items[i]
			if ev.InvolvedObject.Kind != "Deployment" && ev.InvolvedObject.Kind != "Pod" && ev.InvolvedObject.Kind != "ReplicaSet" {
				continue
			}
			if !strings.HasPrefix(ev.InvolvedObject.Name, sealedSecretsControllerDeploymentName) {
				continue
			}
			push(fmt.Sprintf("Event %s/%s [%s] reason=%s message=%s",
				ev.InvolvedObject.Kind, ev.InvolvedObject.Name,
				ev.Type, ev.Reason, truncate(ev.Message)))
			relevant++
			if relevant >= 10 { // cap the diagnostic length
				push("(... more events truncated ...)")
				break
			}
		}
	}

	if len(lines) == 0 {
		return "  (no diagnostic information available)"
	}
	return strings.Join(lines, "\n")
}

func deploymentDesiredReplicas(dep *appsV1.Deployment) int32 {
	if dep.Spec.Replicas == nil {
		return 1
	}
	return *dep.Spec.Replicas
}

// diagnosticMessageMaxLen caps how much of a Kubernetes status Message
// gets folded into a diagnostic line — long messages bloat the report.
const diagnosticMessageMaxLen = 200

func truncate(s string) string {
	if len(s) <= diagnosticMessageMaxLen {
		return s
	}
	return s[:diagnosticMessageMaxLen] + "…"
}

// describeSealedSecretsPods renders diagnostic lines for the
// sealed-secrets-controller pods — phase, conditions, and per-container
// state — for diagnoseSealedSecretsController to fold into its report.
// Lines are returned un-indented; the caller's push adds the indent.
func describeSealedSecretsPods(pods []coreV1.Pod) []string {
	var lines []string
	for i := range pods {
		p := &pods[i]
		lines = append(lines, fmt.Sprintf("Pod %s phase=%s nodeName=%s",
			p.Name, p.Status.Phase, p.Spec.NodeName))

		for _, cond := range p.Status.Conditions {
			lines = append(lines, fmt.Sprintf("  Pod condition %s=%s reason=%s message=%s",
				cond.Type, cond.Status, cond.Reason, truncate(cond.Message)))
		}

		for _, cs := range p.Status.ContainerStatuses {
			lines = append(lines, fmt.Sprintf("  Container %s ready=%t restartCount=%d image=%s",
				cs.Name, cs.Ready, cs.RestartCount, cs.Image))
			if cs.State.Waiting != nil {
				lines = append(lines, fmt.Sprintf("    state=waiting reason=%s message=%s",
					cs.State.Waiting.Reason, truncate(cs.State.Waiting.Message)))
			}
			if cs.State.Terminated != nil {
				lines = append(lines, fmt.Sprintf("    state=terminated reason=%s exitCode=%d message=%s",
					cs.State.Terminated.Reason, cs.State.Terminated.ExitCode,
					truncate(cs.State.Terminated.Message)))
			}
			if cs.LastTerminationState.Terminated != nil {
				lines = append(lines, fmt.Sprintf("    lastTerminated reason=%s exitCode=%d message=%s",
					cs.LastTerminationState.Terminated.Reason,
					cs.LastTerminationState.Terminated.ExitCode,
					truncate(cs.LastTerminationState.Terminated.Message)))
			}
		}
	}
	return lines
}
