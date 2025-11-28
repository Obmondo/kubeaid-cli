// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (h *Hetzner) PointFailoverIPToInitMasterNode(ctx context.Context) {
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

	// Get IP address of the server, to which the Failover IP is currently pointing.
	activeServerIP := h.getActiveServerIP(ctx, failoverIP)
	slog.InfoContext(ctx,
		"Detected active server IP for failover IP",
		slog.String("ip", activeServerIP),
	)

	// Detect the 'init master node' IP.
	// 'init master node' is the first master node, where 'kubeadm init' is executed.
	initMasterNodeIP := getInitMasterNodeIP(ctx)

	// The failover IP is already pointing to the 'init master node'.
	// So, we don't need to do anything.
	if activeServerIP == initMasterNodeIP {
		slog.InfoContext(ctx, "Failover IP is already pointing to the 'init master node'")
		return
	}

	// Otherwise, make the Failover IP point to the 'init master node'.
	h.pointFailoverIPTo(ctx, failoverIP, initMasterNodeIP)
}

type (
	GetFailoverIPDetailsResponse struct {
		Failover FailoverIPDetails `json:"failover"`
	}

	FailoverIPDetails struct {
		ActiveServerIP string `json:"active_server_ip"`
	}
)

// Returns the IP address of the server, the given Failover IP is pointing to.
func (h *Hetzner) getActiveServerIP(ctx context.Context, failoverIP string) string {
	response, err := h.robotClient.NewRequest().Get("/failover/" + failoverIP)

	assert.AssertErrNil(ctx, err, "Failed getting Failover IP details")
	assert.Assert(ctx, response.StatusCode() == http.StatusOK, "Failed getting Failover IP details")

	var unmarshalledResponse GetFailoverIPDetailsResponse
	err = json.Unmarshal(response.Body(), &unmarshalledResponse)
	assert.AssertErrNil(ctx, err, "Failed unmarshalling Failover IP details")

	return unmarshalledResponse.Failover.ActiveServerIP
}

// Returns the public IP of the 'init master node'.
func getInitMasterNodeIP(ctx context.Context) string {
	// Construct cluster client.
	kubeconfig := utils.MustGetEnv(constants.EnvNameKubeconfig)
	clusterClient := kubernetes.MustCreateClusterClient(ctx, kubeconfig)

	var initMasterNodeIP string

	_ = wait.PollUntilContextCancel(ctx, 5*time.Second, false,
		func(ctx context.Context) (bool, error) {
			// Get the HetznerBareMetalMachines.
			hetznerBareMetalMachines := &caphV1Beta1.HetznerBareMetalMachineList{}
			err := clusterClient.List(ctx, hetznerBareMetalMachines, &client.ListOptions{
				Namespace: kubernetes.GetCapiClusterNamespace(),
			})
			assert.AssertErrNil(ctx, err, "Failed listing HetznerBareMetalMachines")

			// Any HetznerBareMetalMachine has still not been created.
			// Wait for sometime and check again.
			if len(hetznerBareMetalMachines.Items) == 0 {
				return false, nil
			}

			// Filter out the HetznerBareMetalMachines corresponding to worker nodes.
			// They won't have the cluster.x-k8s.io/control-plane label.
			hetznerBareMetalMachines.Items = slices.DeleteFunc(hetznerBareMetalMachines.Items,
				func(hetznerBareMetalMachine caphV1Beta1.HetznerBareMetalMachine) bool {
					_, exists := hetznerBareMetalMachine.Labels[clusterAPIV1Beta1.MachineControlPlaneLabel]
					return !exists
				},
			)

			// Sort the HetznerBareMetalMachines in ascending order, by the time of creation.
			// The oldest HetznerBareMetalMachine corresponds to the 'init master node'.
			sort.Slice(hetznerBareMetalMachines.Items, func(i, j int) bool {
				a := hetznerBareMetalMachines.Items[i]
				b := hetznerBareMetalMachines.Items[j]

				return a.CreationTimestamp.Before(&b.CreationTimestamp)
			})

			initMasterNodeHetznerBareMetalMachine := hetznerBareMetalMachines.Items[0]

			// Now, that we have detected the HetznerBareMetalMachine that corresponds to the 'init master node',
			// let's get the corresponding HetznerBareMetalHost.

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
			err = kubernetes.GetKubernetesResource(
				ctx,
				clusterClient,
				initMasterNodeHetznerBareMetalHost,
			)
			assert.AssertErrNil(ctx, err,
				"Failed getting HetznerBareMetalHost corresponding to the 'init master node'",
			)

			initMasterNodeIP = initMasterNodeHetznerBareMetalHost.Spec.Status.IPv4
			if len(initMasterNodeIP) == 0 {
				return false, nil
			}

			return true, nil
		},
	)

	return initMasterNodeIP
}

// Makes the Failover IP point to the given server.
func (h *Hetzner) pointFailoverIPTo(ctx context.Context, failoverIP, targetServerIP string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-ip", targetServerIP),
	})

	response, err := h.robotClient.NewRequest().
		SetFormData(map[string]string{
			"active_server_ip": targetServerIP,
		}).
		Post("/failover/" + failoverIP)

	assert.AssertErrNil(ctx, err, "Failed pointing the Failover IP to the given server IP")
	assert.Assert(ctx,
		(response.StatusCode() == http.StatusOK),
		"Failed pointing the Failover IP to the given server IP",
		slog.Any("response", response),
	)

	slog.InfoContext(ctx, "Successfully pointed the Failover IP to the given server IP")
}
