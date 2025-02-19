package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/floshodan/hrobot-go/hrobot"
)

func main() {
	ctx := context.Background()

	// Read required environment variables.
	var (
		failoverIP = utils.GetEnv("FAILOVER_IP")

		nodeIP = utils.GetEnv("NODE_IP")

		username = os.Getenv("API_USERNAME") // (optional).
		password = os.Getenv("API_PASSWORD") // (optional).

		apiToken = os.Getenv("API_TOKEN") // (optional).
	)

	// Construct Hetzner Robot API client.
	var hetznerRobotClient *hrobot.Client
	switch {
	case len(username) > 0 && len(password) > 0:
		hetznerRobotClient = hrobot.NewClient(hrobot.WithBasicAuth(username, password))

	case len(apiToken) > 0:
		hetznerRobotClient = hrobot.NewClient(hrobot.WithToken(apiToken))

	default:
		log.Fatalf("Either provide username and password / api token as credentials, to communicate with the Hetzner Robot API")
	}

	/*
		A Failover IP is an additional IP that you can switch from one server to another. You can order
		it for any Hetzner dedicated root server, and you can switch it to any other Hetzner dedicated
		root server, regardless of location.

		Switching a failover IP takes between 90 and 110 seconds.

		REFERENCE : https://docs.hetzner.com/robot/dedicated-server/ip/failover/.
	*/
	// Hetzner Robot Failover IP API spec : API REFERENCE : https://robot.hetzner.com/doc/webservice/en.html#failover.

	// Get the Failover IP's current active server IP.
	failoverIPDetails, _, err := hetznerRobotClient.Failover.GetFailoverIP(ctx, failoverIP)
	assert.AssertErrNil(ctx, err, "Failed getting Failover IP details")

	activeServerIP := failoverIPDetails.ActiveServerIP
	slog.InfoContext(ctx, "Detected active server", slog.String("ip", activeServerIP))

	if activeServerIP == nodeIP {
		slog.InfoContext(ctx, "Active server IP is already same as the current server IP")
		return
	}

	// Update Failover IP to the current node's IP (the current node, on which this script is
	// running)
	// NOTE : Contributed :
	//				https://github.com/floshodan/hrobot-go/commit/700f8ef9fdac565129608b3a50583b4b6564ff34.
	_, _, err = hetznerRobotClient.Failover.SwitchFailover(ctx, failoverIP, activeServerIP)
	assert.AssertErrNil(ctx, err, "Failed switching Failover IP to the current node IP")

	// Wait for the update to complete.
	for {
		failoverIPDetails, _, err := hetznerRobotClient.Failover.GetFailoverIP(ctx, failoverIP)
		assert.AssertErrNil(ctx, err, "Failed getting Failover IP details")

		if failoverIPDetails.ActiveServerIP == nodeIP {
			slog.InfoContext(ctx, "Successfully updated Failover IP", slog.String("active-server-ip", nodeIP))
			break
		}

		slog.InfoContext(ctx, "Waiting for the Failover IP update to complete. Sleeping for a minute....")
		time.Sleep(time.Minute)
	}
}
