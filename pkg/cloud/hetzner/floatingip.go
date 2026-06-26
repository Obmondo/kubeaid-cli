// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"k8s.io/utils/ptr"
)

// CreateCoturnFloatingIP ensures an HCloud Floating IP exists for the
// cluster's NetBird Coturn (STUN/TURN) HA, and returns its address.
//
// The IP is created UNASSIGNED: hcloud-fip-controller reassigns it to the
// active control-plane node at runtime, and the capi-cluster chart's
// netplan block binds it (/32) on every CP node's primary interface so
// the kernel answers for it once hcloud routes it there. homeLocation
// must sit in the cluster's network zone (a control-plane region) so the
// IP can move across the CP nodes.
//
// Idempotent: looked up by its deterministic name, so bootstrap re-runs
// reuse the same IP instead of allocating (and billing for) a new one.
// Deletion-protected like the control-plane LB — a Floating IP is billed
// and outlives a single bootstrap, so teardown is a deliberate manual
// step rather than something a re-run can undo.
func (h *Hetzner) CreateCoturnFloatingIP(
	ctx context.Context, clusterName, homeLocation string,
) (string, error) {
	existing, err := h.getCoturnFloatingIP(ctx, clusterName)
	if err != nil {
		return "", fmt.Errorf("checking for existing Coturn Floating IP: %w", err)
	}
	if existing != nil {
		slog.InfoContext(ctx, "Coturn Floating IP already exists")
		return existing.IP.String(), nil
	}

	result, response, err := h.floatingIPClient.Create(ctx, hcloud.FloatingIPCreateOpts{
		Type:         hcloud.FloatingIPTypeIPv4,
		HomeLocation: &hcloud.Location{Name: homeLocation},
		Name:         ptr.To(coturnFloatingIPName(clusterName)),
		Description: ptr.To(fmt.Sprintf(
			"NetBird Coturn (STUN/TURN) Floating IP for the %s cluster", clusterName,
		)),
		Labels: map[string]string{
			coturnFloatingIPOwnershipLabel(clusterName): "owned",
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating Coturn Floating IP: %w", err)
	}
	if response == nil {
		return "", fmt.Errorf("creating Coturn Floating IP: nil response")
	}
	if response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("creating Coturn Floating IP: unexpected status %d", response.StatusCode)
	}
	slog.InfoContext(ctx, "Created Coturn Floating IP")

	// Protect the Floating IP from accidental deletion.
	_, response, err = h.floatingIPClient.ChangeProtection(ctx, result.FloatingIP,
		hcloud.FloatingIPChangeProtectionOpts{Delete: ptr.To(true)},
	)
	if err != nil {
		return "", fmt.Errorf("enabling deletion protection on Coturn Floating IP: %w", err)
	}
	// HCloud's ChangeProtection endpoint creates an Action and returns
	// 201 Created, not 200 OK. Accept both so we don't flag success as
	// an error.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("enabling deletion protection on Coturn Floating IP: unexpected status %d", response.StatusCode)
	}
	slog.InfoContext(ctx, "Enabled deletion protection on Coturn Floating IP")

	return result.FloatingIP.IP.String(), nil
}

// getCoturnFloatingIP returns the cluster's Coturn Floating IP if it
// already exists, else nil. Looked up by the deterministic name so the
// create path stays idempotent across bootstrap re-runs.
func (h *Hetzner) getCoturnFloatingIP(
	ctx context.Context, clusterName string,
) (*hcloud.FloatingIP, error) {
	floatingIP, response, err := h.floatingIPClient.GetByName(ctx, coturnFloatingIPName(clusterName))
	if err != nil {
		return nil, fmt.Errorf("running Coturn Floating IP GET operation: %w", err)
	}
	if response != nil && response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("running Coturn Floating IP GET operation: unexpected status %d", response.StatusCode)
	}
	return floatingIP, nil
}

// coturnFloatingIPName is the deterministic Floating IP name, used as the
// idempotency key — one Coturn Floating IP per cluster.
func coturnFloatingIPName(clusterName string) string {
	return fmt.Sprintf("%s-coturn", clusterName)
}

// coturnFloatingIPOwnershipLabel tags the Floating IP as kubeaid-cli-owned
// for the given cluster, mirroring the control-plane LB ownership label.
func coturnFloatingIPOwnershipLabel(clusterName string) string {
	return fmt.Sprintf("caph-cluster-%s", clusterName)
}
