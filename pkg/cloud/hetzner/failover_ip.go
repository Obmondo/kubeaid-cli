// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

func (h *Hetzner) PointFailoverIPToInitMasterNode(ctx context.Context) error {
	/*
		A Failover IP is an additional IP that you can switch from one server to another. You can order
		it for any Hetzner dedicated root server, and you can switch it to any other Hetzner dedicated
		root server, regardless of location.

		Switching a Failover IP takes between 90 and 110 seconds.

		REFERENCE : https://docs.hetzner.com/robot/dedicated-server/ip/failover/.

		You can find the Hetzner Robot Failover IP API spec here :
		https://robot.hetzner.com/doc/webservice/en.html#failover.
	*/

	failoverIP := config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.BareMetal.Endpoint.Host

	activeServerIP, err := h.getActiveServerIP(ctx, failoverIP)
	if err != nil {
		return fmt.Errorf("getting active server IP for failover IP: %w", err)
	}
	slog.InfoContext(ctx,
		"Detected active server IP for failover IP",
		slog.String("ip", activeServerIP),
	)

	initMasterNodeIP, err := getInitMasterNodeIP(ctx)
	if err != nil {
		return fmt.Errorf("detecting init master node IP: %w", err)
	}

	if activeServerIP == initMasterNodeIP {
		slog.InfoContext(ctx, "Failover IP is already pointing to the 'init master node'")
		return nil
	}

	if err := h.pointFailoverIPTo(ctx, failoverIP, initMasterNodeIP); err != nil {
		return fmt.Errorf("pointing failover IP to init master node: %w", err)
	}

	return nil
}

type (
	GetFailoverIPDetailsResponse struct {
		Failover FailoverIPDetails `json:"failover"`
	}

	FailoverIPDetails struct {
		ActiveServerIP string `json:"active_server_ip"`
	}
)

func (h *Hetzner) getActiveServerIP(ctx context.Context, failoverIP string) (string, error) {
	response, err := h.robotClient.NewRequest().SetContext(ctx).Get("/failover/" + failoverIP)
	if err != nil {
		return "", fmt.Errorf("requesting failover IP details: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d when getting failover IP details: %s",
			response.StatusCode(), hRobotErrorMessage(response.Body()))
	}

	var unmarshalledResponse GetFailoverIPDetailsResponse
	if err := json.Unmarshal(response.Body(), &unmarshalledResponse); err != nil {
		return "", fmt.Errorf("unmarshalling failover IP details: %w", err)
	}

	return unmarshalledResponse.Failover.ActiveServerIP, nil
}

func getInitMasterNodeIP(ctx context.Context) (string, error) {
	kubeconfig := utils.MustGetEnv(constants.EnvNameKubeconfig)
	clusterClient, err := kubernetes.CreateKubernetesClient(ctx, kubeconfig)
	if err != nil {
		return "", fmt.Errorf("constructing Kubernetes cluster client: %w", err)
	}

	var initMasterNodeIP string

	pollErr := wait.PollUntilContextCancel(ctx, 5*time.Second, false,
		func(ctx context.Context) (bool, error) {
			hetznerBareMetalMachines := &caphV1Beta1.HetznerBareMetalMachineList{}
			if err := clusterClient.List(ctx, hetznerBareMetalMachines, &client.ListOptions{
				Namespace: kubernetes.GetCapiClusterNamespace(),
			}); err != nil {
				return false, fmt.Errorf("listing HetznerBareMetalMachines: %w", err)
			}

			if len(hetznerBareMetalMachines.Items) == 0 {
				return false, nil
			}

			hetznerBareMetalMachines.Items = slices.DeleteFunc(hetznerBareMetalMachines.Items,
				func(hetznerBareMetalMachine caphV1Beta1.HetznerBareMetalMachine) bool {
					_, exists := hetznerBareMetalMachine.Labels[clusterAPIV1Beta1.MachineControlPlaneLabel]
					return !exists
				},
			)

			sort.Slice(hetznerBareMetalMachines.Items, func(i, j int) bool {
				a := hetznerBareMetalMachines.Items[i]
				b := hetznerBareMetalMachines.Items[j]

				return a.CreationTimestamp.Before(&b.CreationTimestamp)
			})

			initMasterNodeHetznerBareMetalMachine := hetznerBareMetalMachines.Items[0]

			hostAnnotation, ok := initMasterNodeHetznerBareMetalMachine.Annotations[caphV1Beta1.HostAnnotation]
			if !ok {
				return false, nil
			}

			hostAnnotationParts := strings.Split(hostAnnotation, "/")

			initMasterNodeHetznerBareMetalHost := &caphV1Beta1.HetznerBareMetalHost{
				ObjectMeta: metaV1.ObjectMeta{
					Namespace: hostAnnotationParts[0],
					Name:      hostAnnotationParts[1],
				},
			}
			if err := kubernetes.GetKubernetesResource(
				ctx,
				clusterClient,
				initMasterNodeHetznerBareMetalHost,
			); err != nil {
				return false, fmt.Errorf("getting HetznerBareMetalHost for init master node: %w", err)
			}

			initMasterNodeIP = initMasterNodeHetznerBareMetalHost.Spec.Status.IPv4
			if len(initMasterNodeIP) == 0 {
				return false, nil
			}

			return true, nil
		},
	)
	if pollErr != nil {
		return "", fmt.Errorf("polling for init master node IP: %w", pollErr)
	}

	if initMasterNodeIP == "" {
		return "", fmt.Errorf("init master node IP is empty after polling completed")
	}

	return initMasterNodeIP, nil
}

// pointFailoverIPTo switches failoverIP to targetServerIP.
//
// The switch takes 90-110s inside Robot, which makes the POST's own
// response unreliable as a success signal: the client can time out
// mid-switch, and a request issued while a switch is still being
// applied comes back 409. Neither means the switch failed. So the POST
// is treated as "best-effort kick" and the actual gate is
// waitForFailoverIP — GET /failover/{ip} until it reports the target
// as its active server.
//
// Only an unambiguous rejection (4xx that isn't 409, 5xx after the
// client's own retries) fails fast without polling.
func (h *Hetzner) pointFailoverIPTo(ctx context.Context, failoverIP, targetServerIP string) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-ip", targetServerIP),
	})

	slog.InfoContext(ctx, "Pointing the Failover IP to the given server IP (Hetzner takes 90-110s to switch)")

	response, err := h.failoverClient().NewRequest().
		SetContext(ctx).
		SetFormData(map[string]string{
			"active_server_ip": targetServerIP,
		}).
		Post("/failover/" + failoverIP)

	switch {
	case err != nil:
		// Transport error or client-side timeout. Robot may well have
		// accepted the switch — verify where the IP points before
		// calling this a failure.
		slog.WarnContext(ctx, "Failover IP switch request did not complete cleanly; verifying against Robot",
			slog.Any("error", err),
		)

	case response.StatusCode() == http.StatusOK:
		slog.InfoContext(ctx, "Robot accepted the Failover IP switch")

	case response.StatusCode() == http.StatusConflict:
		// FAILOVER_ALREADY_ROUTED ("it's already there") and
		// FAILOVER_LOCKED ("a switch is in flight") are both answered
		// by the same poll. Any other 409 gets polled too — if the IP
		// does end up on the target, the bootstrap has what it needs.
		slog.WarnContext(ctx, "Robot returned 409 for the Failover IP switch; verifying where the IP actually points",
			slog.String("robot-error-code", hRobotErrorCode(response.Body())),
			slog.String("robot-error-message", hRobotErrorMessage(response.Body())),
		)

	default:
		return fmt.Errorf("unexpected status %d when pointing failover IP to server %s: %s",
			response.StatusCode(), targetServerIP, hRobotErrorMessage(response.Body()))
	}

	return h.waitForFailoverIP(ctx, failoverIP, targetServerIP)
}

// waitForFailoverIP polls Robot until failoverIP reports targetServerIP
// as its active server. This is the real success gate for a switch —
// see pointFailoverIPTo.
func (h *Hetzner) waitForFailoverIP(ctx context.Context, failoverIP, targetServerIP string) error {
	maxWait := h.failoverMaxWait
	if maxWait <= 0 {
		maxWait = constants.HRobotFailoverMaxWaitTime
	}
	deadline := time.Now().Add(maxWait)

	for {
		var reason error

		activeServerIP, err := h.getActiveServerIP(ctx, failoverIP)
		switch {
		case err != nil:
			reason = err

		case activeServerIP == targetServerIP:
			slog.InfoContext(ctx, "Successfully pointed the Failover IP to the given server IP")
			return nil

		default:
			reason = fmt.Errorf("failover IP still points to %q", activeServerIP)
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("aborted while waiting for failover IP %s to point to %s (%v): %w",
				failoverIP, targetServerIP, reason, ctxErr)
		}

		if !time.Now().Before(deadline) {
			return fmt.Errorf("timed out after %v waiting for failover IP %s to point to %s: %w",
				maxWait, failoverIP, targetServerIP, reason)
		}

		slog.InfoContext(ctx, "Waiting for the Failover IP switch to complete...",
			slog.Duration("interval", constants.HRobotFailoverPollInterval),
			slog.Any("reason", reason),
		)
		h.sleepFunc(constants.HRobotFailoverPollInterval)
	}
}

// failoverClient returns the Robot client to use for the failover
// switch POST — a long-timeout, no-retry clone of the shared one.
// Falls back to the shared client when the long-timeout one wasn't
// built (tests construct Hetzner directly).
func (h *Hetzner) failoverClient() *resty.Client {
	if h.robotFailoverClient != nil {
		return h.robotFailoverClient
	}
	return h.robotClient
}
