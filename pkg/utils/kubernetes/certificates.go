// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// certManagerCertificateGVK is the GroupVersionKind of cert-manager's
// Certificate CRD. We read it through the unstructured client so
// kubeaid-cli doesn't have to vendor the cert-manager API module — a
// `kubectl get certificate`-equivalent is all the bootstrap needs.
var certManagerCertificateGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "Certificate",
}

// Poll cadence + cap for WaitForCertificatesReady. ACME HTTP-01
// issuance (present challenge → Let's Encrypt validation → finalize)
// usually finishes within a minute or two; 10 minutes is a generous
// safety net for slow DNS propagation or cert-manager's retry backoff.
// Package-level vars so tests can shrink them.
var (
	waitForCertificatesReadyTimeout      = 10 * time.Minute
	waitForCertificatesReadyPollInterval = 10 * time.Second
)

// WaitForCertificatesReady blocks until every Certificate in certs
// reports Ready=True, ctx is cancelled, or waitForCertificatesReadyTimeout
// passes. An empty certs slice is a no-op.
//
// On a VPN cluster the netbird / keycloak workloads are unusable until
// Traefik serves a real TLS cert for their FQDNs — netbird-management,
// for instance, crashloops on the OIDC discovery fetch while Traefik
// falls back to its self-signed default. Gating the bootstrap here
// turns that cryptic downstream x509 crashloop into a clear
// "cert <name> is not Ready: <reason>" failure that points the operator
// straight at cert-manager's Order / Challenge.
//
// cert-manager retries failed issuance with backoff, so a transient
// failure resolves on its own — the loop keeps polling and only gives
// up (with the last-seen reason) at the timeout. Certificates are read
// via the unstructured client (GVK cert-manager.io/v1 Certificate); a
// Certificate that doesn't exist yet counts as not-ready, since
// cert-manager's ingress-shim creates it once the Ingress is synced.
func WaitForCertificatesReady(
	ctx context.Context,
	kubeClient client.Client,
	certs []types.NamespacedName,
) error {
	if len(certs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, waitForCertificatesReadyTimeout)
	defer cancel()

	for {
		var notReady []string
		for _, certKey := range certs {
			if ready, detail := isCertificateReady(ctx, kubeClient, certKey); !ready {
				notReady = append(notReady,
					fmt.Sprintf("%s/%s (%s)", certKey.Namespace, certKey.Name, detail),
				)
			}
		}

		if len(notReady) == 0 {
			slog.InfoContext(ctx, "All TLS Certificates are Ready")
			return nil
		}

		slog.InfoContext(ctx, "Waiting for TLS Certificates to be issued by cert-manager",
			slog.Any("not_ready", notReady),
		)

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"timed out after %s waiting for TLS Certificates to be Ready: %v — "+
					"cert-manager couldn't issue them. Inspect the chain with "+
					"`kubectl describe certificate,certificaterequest,order,challenge -A`",
				waitForCertificatesReadyTimeout, notReady,
			)
		case <-time.After(waitForCertificatesReadyPollInterval):
		}
	}
}

// isCertificateReady reports whether the named cert-manager Certificate
// has a Ready condition with status "True". The second return value is
// a short human-readable detail for the not-ready case.
//
// On failure it prefers the Issuing condition's reason/message over
// Ready's: a stuck Certificate carries Ready=False with the unhelpful
// reason "DoesNotExist" (the Secret just isn't there yet), while the
// Issuing=False/reason=Failed condition is where cert-manager records
// *why* the last attempt failed.
func isCertificateReady(
	ctx context.Context,
	kubeClient client.Client,
	certKey types.NamespacedName,
) (bool, string) {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certManagerCertificateGVK)

	if err := kubeClient.Get(ctx, certKey, cert); err != nil {
		if k8sAPIErrors.IsNotFound(err) {
			return false, "not created yet"
		}
		return false, fmt.Sprintf("read error: %v", err)
	}

	conditions, found, err := unstructured.NestedSlice(cert.Object, "status", "conditions")
	if err != nil || !found {
		return false, "no status conditions yet"
	}

	var readyCond, issuingCond map[string]any
	for _, c := range conditions {
		condition, ok := c.(map[string]any)
		if !ok {
			continue
		}
		switch condition["type"] {
		case "Ready":
			readyCond = condition
		case "Issuing":
			issuingCond = condition
		}
	}

	if readyCond != nil && readyCond["status"] == "True" {
		return true, ""
	}

	// Not Ready. If the last issuance attempt failed, that condition
	// holds the real reason — surface it (with the attempt count so a
	// genuinely-stuck cert is distinguishable from a slow first try).
	if issuingCond != nil &&
		issuingCond["status"] == "False" && issuingCond["reason"] == "Failed" {
		detail := conditionDetail(issuingCond)
		if attempts, ok, _ := unstructured.NestedInt64(
			cert.Object, "status", "failedIssuanceAttempts",
		); ok && attempts > 0 {
			detail = fmt.Sprintf("%s [failedIssuanceAttempts=%d]", detail, attempts)
		}
		return false, detail
	}

	// Still issuing / no failure recorded yet — the Ready message
	// ("DoesNotExist", "Issuing certificate as Secret does not exist")
	// is the best we have.
	if readyCond != nil {
		return false, conditionDetail(readyCond)
	}
	return false, "no Ready condition yet"
}

// conditionDetail formats a Kubernetes-style condition's reason +
// message into a single human-readable line.
func conditionDetail(condition map[string]any) string {
	reason, _ := condition["reason"].(string)
	message, _ := condition["message"].(string)
	switch {
	case reason != "" && message != "":
		return reason + ": " + message
	case message != "":
		return message
	case reason != "":
		return reason
	default:
		return "Ready!=True"
	}
}
