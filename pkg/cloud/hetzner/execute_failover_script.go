package hetzner

import "context"

/*
Let's say there are x (> 1) master nodes behind the Failover IP.

CAPH (Cluster API Provider Hetzner) will SSH into any one of those master nodes (let's denote
it by β) and execute `kubeadm init`. The Machine resource corresponding to β will then be
marked as ready. We'll wait for this event.

Once this event occurs, we'll SSH into β and run the Hetzner Failover Script (using the
hetzner-robot App in KubeAid). This will make the Failover IP point to β.

The Kubernetes API server of the provisioned cluster will then be reachable via that Failover
IP.
*/
func ExecuteFailoverScript(ctx context.Context) {
	panic("unimplemented")
}
