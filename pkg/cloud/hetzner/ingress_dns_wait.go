// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

// ingressLBPollInterval / ingressLBPollTimeout cap the wait for
// Traefik's Service to get a public IP from CCM. Hetzner CCM
// typically allocates within ~30s; ten minutes is a generous
// safety net for slow control-plane reconciliation or network
// blips.
const (
	ingressLBPollInterval = 5 * time.Second
	ingressLBPollTimeout  = 10 * time.Minute
)

// WaitForIngressLBDNS gates bootstrap on the operator pointing the
// public-facing FQDNs (keycloak.dns / netbird.dns / netbird.stunDNS
// / netbird.turnDNS) at the Traefik LB's public IP. Run as
// SyncAllArgoCDApps's beforeRemainingApps gate — after ccm + traefik
// are synced (so the LB is being provisioned) but before the
// application-layer apps (netbird, keycloakx) whose Ingress
// certificates depend on DNS resolving. We poll the Service for
// status.loadBalancer.ingress[0].ip and only then prompt the operator.
//
// cert-manager's ACME challenges for keycloakx / netbird Ingresses
// retry with exponential backoff. Putting the DNS prompt here lets
// the next retry succeed instead of leaving the apps unhealthy
// while the operator scrambles to figure out which IP to point at.
//
// No-op when no FQDNs are configured (workload clusters, or VPN
// clusters with no Keycloak/NetBird DNS set).
func WaitForIngressLBDNS(ctx context.Context, clusterClient client.Client) error {
	fqdns := ingressLBFQDNs()
	if len(fqdns) == 0 {
		return nil
	}

	bar := progress.FromCtx(ctx)
	bar.Substep("Waiting for Traefik LB to be assigned a public IP")
	lbIP, err := waitForTraefikLBIP(ctx, clusterClient)
	if err != nil {
		return fmt.Errorf("waiting for Traefik LB public IP: %w", err)
	}
	slog.InfoContext(ctx, "Discovered Traefik LB public IP",
		slog.String("ip", lbIP),
	)

	return WaitForDNSResolution(ctx, fqdns, lbIP)
}

// ingressLBFQDNs returns the FQDNs that should resolve to the
// Traefik LB's public IP. Pulled out for testability and to keep
// the spec close to its source — anything new that fronts on
// Traefik (whoami, future apps) extends this list.
func ingressLBFQDNs() []string {
	cluster := config.ParsedGeneralConfig.Cluster
	var fqdns []string
	if cluster.Keycloak != nil && cluster.Keycloak.DNS != "" {
		fqdns = append(fqdns, cluster.Keycloak.DNS)
	}
	if cluster.NetBird != nil {
		if cluster.NetBird.DNS != "" {
			fqdns = append(fqdns, cluster.NetBird.DNS)
		}
		if cluster.NetBird.StunDNS != "" {
			fqdns = append(fqdns, cluster.NetBird.StunDNS)
		}
		if cluster.NetBird.TurnDNS != "" {
			fqdns = append(fqdns, cluster.NetBird.TurnDNS)
		}
	}
	return fqdns
}

// waitForTraefikLBIP polls the traefik namespace for a Service of
// type LoadBalancer with a populated status.loadBalancer.ingress[].ip.
// We list-by-namespace + filter rather than Get-by-name so the wait
// survives the chart's release name changing (kubeaid-cli wraps
// the upstream traefik chart, and the actual Service name depends
// on its release configuration).
//
// While polling, we surface the latest SyncLoadBalancerFailed Event
// from the LB Service (typically posted by the cloud-controller-manager
// when it can't reconcile the LB — e.g. "cloud target was not found"
// when the node isn't in the LB's private network). Live-logging the
// message lets the operator diagnose a wedged CCM during the wait
// rather than after a silent timeout. The same message is folded
// into the final error so the timeout is self-describing.
func waitForTraefikLBIP(ctx context.Context, clusterClient client.Client) (string, error) {
	var (
		ip          string
		lastLogged  string
		lastWarning string
	)
	pollErr := wait.PollUntilContextTimeout(ctx,
		ingressLBPollInterval, ingressLBPollTimeout, true,
		func(ctx context.Context) (bool, error) {
			services := &coreV1.ServiceList{}
			if err := clusterClient.List(ctx, services,
				client.InNamespace(constants.NamespaceTraefik),
			); err != nil {
				slog.WarnContext(ctx,
					"Listing Services in traefik namespace; will retry",
					slog.Any("err", err),
				)
				return false, nil
			}
			for _, svc := range services.Items {
				if svc.Spec.Type != coreV1.ServiceTypeLoadBalancer {
					continue
				}
				for _, ingress := range svc.Status.LoadBalancer.Ingress {
					if ingress.IP != "" {
						ip = ingress.IP
						return true, nil
					}
				}
				if msg := latestLBSyncFailureMessage(ctx, clusterClient, &svc); msg != "" {
					lastWarning = msg
					if msg != lastLogged {
						lastLogged = msg
						slog.WarnContext(ctx,
							"Traefik LB still pending; CCM reports failure",
							slog.String("namespace", svc.Namespace),
							slog.String("service", svc.Name),
							slog.String("event", msg),
						)
					}
				}
			}
			return false, nil
		},
	)
	if pollErr != nil {
		if lastWarning != "" {
			return "", fmt.Errorf(
				"%w; last CCM event on traefik LB Service: %s",
				pollErr, lastWarning,
			)
		}
		return "", pollErr
	}
	return ip, nil
}

// latestLBSyncFailureMessage returns the message of the most recent
// Warning Event with reason=SyncLoadBalancerFailed on svc, or "" if
// none exist. Used by waitForTraefikLBIP to surface CCM reconciliation
// failures while the LB is stuck without an IP.
func latestLBSyncFailureMessage(
	ctx context.Context,
	clusterClient client.Client,
	svc *coreV1.Service,
) string {
	events := &coreV1.EventList{}
	if err := clusterClient.List(ctx, events,
		client.InNamespace(svc.Namespace),
	); err != nil {
		return ""
	}
	var (
		latestTime time.Time
		latestMsg  string
	)
	for _, e := range events.Items {
		if e.InvolvedObject.UID != svc.UID {
			continue
		}
		if e.Reason != "SyncLoadBalancerFailed" {
			continue
		}
		// Prefer LastTimestamp; fall back to EventTime for events
		// emitted via the newer events.k8s.io API which leaves
		// LastTimestamp zero.
		t := e.LastTimestamp.Time
		if t.IsZero() {
			t = e.EventTime.Time
		}
		if t.After(latestTime) {
			latestTime = t
			latestMsg = e.Message
		}
	}
	return latestMsg
}
