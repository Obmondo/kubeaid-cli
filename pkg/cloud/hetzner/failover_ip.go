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

	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
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

	activeServerIP, err := h.getActiveServerIP(failoverIP)
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

func (h *Hetzner) getActiveServerIP(failoverIP string) (string, error) {
	response, err := h.robotClient.NewRequest().Get("/failover/" + failoverIP)
	if err != nil {
		return "", fmt.Errorf("requesting failover IP details: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d when getting failover IP details", response.StatusCode())
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

func (h *Hetzner) pointFailoverIPTo(ctx context.Context, failoverIP, targetServerIP string) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-ip", targetServerIP),
	})

	response, err := h.robotClient.NewRequest().
		SetFormData(map[string]string{
			"active_server_ip": targetServerIP,
		}).
		Post("/failover/" + failoverIP)
	if err != nil {
		return fmt.Errorf("posting failover IP switch request: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected status %d when pointing failover IP to server %s", response.StatusCode(), targetServerIP)
	}

	slog.InfoContext(ctx, "Successfully pointed the Failover IP to the given server IP")
	return nil
}
