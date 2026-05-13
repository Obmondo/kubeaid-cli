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
// / netbird.turnDNS) at the Traefik LB's public IP. Called after
// SyncAllArgoCDApps returns so Traefik has already started
// provisioning its LB; we poll the Service for status.loadBalancer
// .ingress[0].ip and only then prompt the operator.
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
func waitForTraefikLBIP(ctx context.Context, clusterClient client.Client) (string, error) {
	var ip string
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
			}
			return false, nil
		},
	)
	if pollErr != nil {
		return "", pollErr
	}
	return ip, nil
}
