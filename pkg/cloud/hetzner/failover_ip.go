package hetzner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-resty/resty/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (h *Hetzner) PointFailoverIPToInitMasterNode(ctx context.Context) {
	robotWebServiceUserCredentials := config.ParsedSecretsConfig.Hetzner.Robot

	httpClient := resty.New().
		SetBaseURL(constants.HetznerRobotWebServiceAPI).
		SetBasicAuth(robotWebServiceUserCredentials.User, robotWebServiceUserCredentials.Password)

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
	activeServerIP := getActiveServerIP(ctx, httpClient, failoverIP)
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
	pointFailoverIPTo(ctx, httpClient, failoverIP, initMasterNodeIP)
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
func getActiveServerIP(ctx context.Context, httpClient *resty.Client, failoverIP string) string {
	response, err := httpClient.NewRequest().
		SetHeader("Accept", "application/json").
		Get("/failover/" + failoverIP)

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

	// Get all the HetznerBareMetalHosts.
	// We need to wait until atleast 1 HetznerBareMetalHost exists.
	hetznerBareMetalHosts := &caphV1Beta1.HetznerBareMetalHostList{}
	for {
		err := clusterClient.List(ctx, hetznerBareMetalHosts, &client.ListOptions{
			Namespace: kubernetes.GetCapiClusterNamespace(),
		})
		assert.AssertErrNil(ctx, err, "Failed listing HetznerBareMetalHosts")

		if len(hetznerBareMetalHosts.Items) > 0 {
			break
		}

		time.Sleep(time.Minute)
	}

	// Sort the HetznerBareMetalHosts in ascending order, by the time of creation.
	// The oldest HetznerBareMetalHost corresponds to the 'init master node'.
	sort.Slice(hetznerBareMetalHosts.Items, func(i, j int) bool {
		a := hetznerBareMetalHosts.Items[i]
		b := hetznerBareMetalHosts.Items[j]

		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	})

	// Get the 'init master node's IP.

	initMasterNodeHetznerBareMetalHost := hetznerBareMetalHosts.Items[0]

	return initMasterNodeHetznerBareMetalHost.Spec.Status.IPv4
}

// Makes the Failover IP point to the given server.
func pointFailoverIPTo(ctx context.Context,
	httpClient *resty.Client,
	failoverIP,
	targetServerIP string,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-ip", targetServerIP),
	})

	response, err := httpClient.NewRequest().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormData(map[string]string{
			"active_server_ip": targetServerIP,
		}).
		SetHeader("Accept", "application/json").
		Post("/failover/" + failoverIP)

	assert.AssertErrNil(ctx, err, "Failed pointing the Failover IP to the given server IP")
	assert.Assert(ctx,
		(response.StatusCode() == http.StatusOK),
		"Failed pointing the Failover IP to the given server IP",
		slog.Any("response", response),
	)

	slog.InfoContext(ctx, "Successfully pointed the Failover IP to the given server IP")
}
