// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	icmpRuleWant          = FirewallRule{Name: "allow-icmp", IPVersion: "ipv4", Protocol: "icmp", Action: "accept"}
	returnTrafficRuleWant = FirewallRule{Name: "allow-return-traffic", IPVersion: "ipv4", DstPort: "32768-65535", Action: "accept"}
	denyAPIServerWant     = FirewallRule{Name: "deny-kube-apiserver", IPVersion: "ipv4", Protocol: "tcp", DstPort: "6443", Action: "discard"}
	allowHTTPWant         = FirewallRule{Name: "allow-http", IPVersion: "ipv4", Protocol: "tcp", DstPort: "80", Action: "accept"}
	allowHTTPSWant        = FirewallRule{Name: "allow-https", IPVersion: "ipv4", Protocol: "tcp", DstPort: "443", Action: "accept"}
)

// TestControlPlaneIngressRuleset pins the control-plane ruleset: SSH (open by
// default, restricted when allowSSHFrom is set), 6443 always denied, 80/443
// always open, allowPublic appended, then ICMP + return traffic.
func TestControlPlaneIngressRuleset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		allowSSHFrom []string
		allowPublic  []config.FirewallPort
		failoverIP   string
		want         []FirewallRule
	}{
		{
			name: "no failover IP: 80/443 unscoped (any dst), SSH open, 6443 denied",
			want: []FirewallRule{
				{Name: "allow-ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "accept"},
				denyAPIServerWant, allowHTTPWant, allowHTTPSWant, icmpRuleWant, returnTrafficRuleWant,
			},
		},
		{
			name:         "failover IP scopes 80/443 + allowPublic to it; allowSSHFrom restricts SSH",
			allowSSHFrom: []string{"203.0.113.4", "198.51.100.0/24"},
			allowPublic:  []config.FirewallPort{{Port: "5432", Protocol: "tcp"}},
			failoverIP:   "192.0.2.1",
			want: []FirewallRule{
				{Name: "allow-ssh-0", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", SrcIP: "203.0.113.4/32", Action: "accept"},
				{Name: "allow-ssh-1", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", SrcIP: "198.51.100.0/24", Action: "accept"},
				{Name: "deny-ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "discard"},
				denyAPIServerWant,
				{Name: "allow-http", IPVersion: "ipv4", Protocol: "tcp", DstPort: "80", DstIP: "192.0.2.1/32", Action: "accept"},
				{Name: "allow-https", IPVersion: "ipv4", Protocol: "tcp", DstPort: "443", DstIP: "192.0.2.1/32", Action: "accept"},
				{Name: "allow-5432-tcp", IPVersion: "ipv4", Protocol: "tcp", DstPort: "5432", DstIP: "192.0.2.1/32", Action: "accept"},
				icmpRuleWant, returnTrafficRuleWant,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ruleset := ControlPlaneIngressRuleset(tc.allowSSHFrom, tc.allowPublic, tc.failoverIP)
			assert.Equal(t, "active", ruleset.Status)
			assert.Equal(t, tc.want, ruleset.Rules)
		})
	}
}

// TestWorkerIngressRuleset pins the worker ruleset: SSH (open by default,
// restricted when allowSSHFrom is set) plus ICMP and return traffic — and
// nothing else (no 80/443, no 6443, no allowPublic).
func TestWorkerIngressRuleset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		allowSSHFrom []string
		want         []FirewallRule
	}{
		{
			name: "defaults: SSH open, nothing else public",
			want: []FirewallRule{
				{Name: "allow-ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "accept"},
				icmpRuleWant, returnTrafficRuleWant,
			},
		},
		{
			name:         "allowSSHFrom restricts SSH",
			allowSSHFrom: []string{"203.0.113.4"},
			want: []FirewallRule{
				{Name: "allow-ssh-0", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", SrcIP: "203.0.113.4/32", Action: "accept"},
				{Name: "deny-ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "discard"},
				icmpRuleWant, returnTrafficRuleWant,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ruleset := WorkerIngressRuleset(tc.allowSSHFrom)
			assert.Equal(t, "active", ruleset.Status)
			assert.Equal(t, tc.want, ruleset.Rules)

			// Workers must never expose 80/443 or the apiserver.
			for _, rule := range ruleset.Rules {
				assert.NotContains(t, []string{"80", "443", "6443"}, rule.DstPort,
					"worker ruleset must not open port %s", rule.DstPort)
			}
		})
	}
}

// TestSSHIngressRules verifies the SSH allow-list logic: empty -> allow from all
// (single rule, no deny); set -> one allow per source (bare IP normalised to /32,
// CIDR kept) followed by a blanket deny so first-match-wins lets the listed
// sources through.
func TestSSHIngressRules(t *testing.T) {
	t.Parallel()

	t.Run("empty allows SSH from all", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []FirewallRule{
			{Name: "allow-ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "accept"},
		}, sshIngressRules(nil))
	})

	t.Run("set restricts to sources then denies the rest", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []FirewallRule{
			{Name: "allow-ssh-0", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", SrcIP: "203.0.113.4/32", Action: "accept"},
			{Name: "allow-ssh-1", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", SrcIP: "198.51.100.0/24", Action: "accept"},
			{Name: "deny-ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "discard"},
		}, sshIngressRules([]string{"203.0.113.4", "198.51.100.0/24"}))
	})
}

// TestFirewallRulesetEqual verifies the nil-vs-empty-slice normalisation in
// firewallRulesetEqual. Robot returns JSON null for an empty rule list, which
// Unmarshal maps to nil; the desired ruleset uses []FirewallRule{}. Both must
// compare equal so EnsureRobotFirewall does not issue a spurious POST.
func TestFirewallRulesetEqual(t *testing.T) {
	t.Parallel()

	rule := FirewallRule{Name: "allow-all", IPVersion: "ipv4", Action: "accept"}

	tests := []struct {
		name  string
		a, b  FirewallRuleset
		equal bool
	}{
		{
			name:  "nil rules vs empty slice — both mean zero rules",
			a:     FirewallRuleset{Status: "active", Rules: nil},
			b:     FirewallRuleset{Status: "active", Rules: []FirewallRule{}},
			equal: true,
		},
		{
			name:  "both nil rules",
			a:     FirewallRuleset{Status: "active", Rules: nil},
			b:     FirewallRuleset{Status: "active", Rules: nil},
			equal: true,
		},
		{
			name:  "both empty slice rules",
			a:     FirewallRuleset{Status: "active", Rules: []FirewallRule{}},
			b:     FirewallRuleset{Status: "active", Rules: []FirewallRule{}},
			equal: true,
		},
		{
			name:  "status mismatch",
			a:     FirewallRuleset{Status: "active"},
			b:     FirewallRuleset{Status: "disabled"},
			equal: false,
		},
		{
			name:  "different rule counts",
			a:     FirewallRuleset{Status: "active", Rules: []FirewallRule{rule}},
			b:     FirewallRuleset{Status: "active", Rules: []FirewallRule{}},
			equal: false,
		},
		{
			name:  "identical non-empty rulesets",
			a:     FirewallRuleset{Status: "active", Rules: []FirewallRule{rule}},
			b:     FirewallRuleset{Status: "active", Rules: []FirewallRule{rule}},
			equal: true,
		},
		{
			name: "same count, different rule content",
			a:    FirewallRuleset{Status: "active", Rules: []FirewallRule{rule}},
			b: FirewallRuleset{Status: "active", Rules: []FirewallRule{
				{Name: "deny-all", IPVersion: "ipv4", Action: "discard"},
			}},
			equal: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.equal, firewallRulesetEqual(tc.a, tc.b))
		})
	}
}

// TestEnsureRobotFirewall_NilRulesIdempotent verifies that EnsureRobotFirewall
// does not POST when desired has []FirewallRule{} and Robot returns null rules
// (which Unmarshal maps to nil). This exercises the nil-vs-empty normalisation
// in firewallRulesetEqual.
func TestEnsureRobotFirewall_NilRulesIdempotent(t *testing.T) {
	t.Parallel()

	const serverIP = "203.0.113.30"

	// desired has an explicit empty (non-nil) slice.
	desired := FirewallRuleset{
		Status: "active",
		Rules:  []FirewallRule{},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewall/"+serverIP:
			// Robot returns null for an empty rule list.
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"firewall":{"server_ip":"203.0.113.30","status":"active","rules":{"input":null}}}`)

		case r.Method == http.MethodPost:
			t.Errorf("unexpected POST: nil-vs-empty normalisation failed, idempotency broken")
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	h, server := newTestHetznerWithRobotServer(handler)
	defer server.Close()

	err := h.EnsureRobotFirewall(context.Background(), serverIP, desired)
	require.NoError(t, err)
}

// robotFirewallJSON constructs the JSON body that Robot returns for GET /firewall/<ip>.
// It marshals a robotFirewallResponse struct literal so field values are safely
// escaped by encoding/json rather than by manual fmt.Sprintf with %q.
func robotFirewallJSON(t *testing.T, serverIP, status string, rules []FirewallRule) string {
	t.Helper()

	resp := robotFirewallResponse{}
	resp.Firewall.ServerIP = serverIP
	resp.Firewall.Status = status
	resp.Firewall.Rules.Input = rules

	b, err := json.Marshal(resp)
	require.NoError(t, err, "robotFirewallJSON: json.Marshal must not fail")
	return string(b)
}

func TestEnsureRobotFirewall(t *testing.T) {
	t.Parallel()

	const serverIP = "203.0.113.10"
	desiredRuleset := ControlPlaneIngressRuleset(nil, nil, "")

	// A minimal ruleset that differs from the default so the diff check triggers a POST.
	differentRuleset := FirewallRuleset{
		Status: "active",
		Rules: []FirewallRule{
			{Name: "allow-all", IPVersion: "ipv4", Action: "accept"},
		},
	}

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "happy path: GET returns different ruleset, POST is called, returns nil",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/firewall/"+serverIP:
					// Return a ruleset that differs from desired.
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, robotFirewallJSON(t, serverIP, differentRuleset.Status, differentRuleset.Rules))

				case r.Method == http.MethodPost && r.URL.Path == "/firewall/"+serverIP:
					require.NoError(t, r.ParseForm())
					// Rule 0 is SSH (allowed from all by default) — accept on port 22.
					assert.Equal(t, "accept", r.PostFormValue("rules[input][0][action]"))
					assert.Equal(t, "22", r.PostFormValue("rules[input][0][dst_port]"))
					// Rule 1 is the apiserver DENY (6443).
					assert.Equal(t, "discard", r.PostFormValue("rules[input][1][action]"))
					assert.Equal(t, "6443", r.PostFormValue("rules[input][1][dst_port]"))
					// Verify the status field.
					assert.Equal(t, "active", r.PostFormValue("status"))
					w.WriteHeader(http.StatusOK)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name: "idempotency: GET returns ruleset equal to desired, no POST, returns nil",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/firewall/"+serverIP:
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, robotFirewallJSON(t, serverIP, desiredRuleset.Status, desiredRuleset.Rules))

				case r.Method == http.MethodPost:
					// Should never be called — fail the test explicitly.
					t.Errorf("unexpected POST to Robot firewall endpoint: idempotency check failed")
					w.WriteHeader(http.StatusOK)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name: "GET error: Robot returns non-200, returns wrapped error naming the server IP",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/firewall/"+serverIP {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: serverIP,
		},
		{
			name: "POST error: Robot returns non-200 on POST, returns wrapped error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/firewall/"+serverIP:
					// Return a different ruleset so the POST is triggered.
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, robotFirewallJSON(t, serverIP, differentRuleset.Status, differentRuleset.Rules))

				case r.Method == http.MethodPost && r.URL.Path == "/firewall/"+serverIP:
					w.WriteHeader(http.StatusBadRequest)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr:    true,
			wantErrMsg: serverIP,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			err := h.EnsureRobotFirewall(context.Background(), serverIP, desiredRuleset)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestEnsureRobotFirewall_FormDataEncoding verifies that all optional fields
// (SrcIP, DstIP, Protocol, DstPort) are omitted from the form-data when empty
// so Robot does not receive spurious empty-string constraints.
func TestEnsureRobotFirewall_FormDataEncoding(t *testing.T) {
	t.Parallel()

	const serverIP = "203.0.113.20"

	// A ruleset whose first rule has only Action and IPVersion set.
	sparseRuleset := FirewallRuleset{
		Status: "active",
		Rules: []FirewallRule{
			{Name: "allow-all", IPVersion: "ipv4", Action: "accept"},
		},
	}

	capturedForm := url.Values{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/firewall/"+serverIP:
			// Return a status that differs (disabled) so POST is triggered.
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, robotFirewallJSON(t, serverIP, "disabled", sparseRuleset.Rules))

		case r.Method == http.MethodPost && r.URL.Path == "/firewall/"+serverIP:
			require.NoError(t, r.ParseForm())
			capturedForm = r.Form
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	h, server := newTestHetznerWithRobotServer(handler)
	defer server.Close()

	err := h.EnsureRobotFirewall(context.Background(), serverIP, sparseRuleset)
	require.NoError(t, err)

	// Optional fields must be absent when the rule left them empty.
	assert.Empty(t, capturedForm.Get("rules[input][0][protocol]"), "protocol must be omitted when empty")
	assert.Empty(t, capturedForm.Get("rules[input][0][dst_port]"), "dst_port must be omitted when empty")
	assert.Empty(t, capturedForm.Get("rules[input][0][src_ip]"), "src_ip must be omitted when empty")
	assert.Empty(t, capturedForm.Get("rules[input][0][dst_ip]"), "dst_ip must be omitted when empty")

	// Required fields must always be present.
	assert.Equal(t, "accept", capturedForm.Get("rules[input][0][action]"))
	assert.Equal(t, "ipv4", capturedForm.Get("rules[input][0][ip_version]"))
}

// TestFirewallEnabled verifies the nil-pointer default: an unset enabled means
// the firewall is managed (true), while an explicit false opts out.
func TestFirewallEnabled(t *testing.T) {
	t.Parallel()

	enabledTrue := true
	enabledFalse := false

	tests := []struct {
		name     string
		firewall config.FirewallConfig
		want     bool
	}{
		{
			name:     "unset (nil) defaults to enabled",
			firewall: config.FirewallConfig{Enabled: nil},
			want:     true,
		},
		{
			name:     "explicit true is enabled",
			firewall: config.FirewallConfig{Enabled: &enabledTrue},
			want:     true,
		},
		{
			name:     "explicit false opts out",
			firewall: config.FirewallConfig{Enabled: &enabledFalse},
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, FirewallEnabled(tc.firewall))
		})
	}
}
