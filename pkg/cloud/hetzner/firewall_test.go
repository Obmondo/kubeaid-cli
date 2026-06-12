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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultBareMetalIngressRuleset pins the exact shape of the default
// inbound ruleset. Any future refactor that silently drops the 6443 DENY
// or changes rule order will be caught here.
func TestDefaultBareMetalIngressRuleset(t *testing.T) {
	t.Parallel()

	ruleset := DefaultBareMetalIngressRuleset()

	assert.Equal(t, "active", ruleset.Status)
	require.Len(t, ruleset.Rules, 6, "expected exactly 6 inbound rules")

	tests := []struct {
		idx      int
		wantRule FirewallRule
	}{
		{
			idx: 0,
			wantRule: FirewallRule{
				Name:      "deny-ssh",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "22",
				Action:    "discard",
			},
		},
		{
			idx: 1,
			wantRule: FirewallRule{
				Name:      "deny-kube-apiserver",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "6443",
				Action:    "discard",
			},
		},
		{
			idx: 2,
			wantRule: FirewallRule{
				Name:      "allow-http",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "80",
				Action:    "accept",
			},
		},
		{
			idx: 3,
			wantRule: FirewallRule{
				Name:      "allow-https",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "443",
				Action:    "accept",
			},
		},
		{
			idx: 4,
			wantRule: FirewallRule{
				Name:      "allow-icmp",
				IPVersion: "ipv4",
				Protocol:  "icmp",
				Action:    "accept",
			},
		},
		{
			idx: 5,
			wantRule: FirewallRule{
				Name:      "allow-return-traffic",
				IPVersion: "ipv4",
				// Protocol is empty: Robot omits it from the POST body, meaning
				// "any protocol". UDP replies (DNS, NTP, NodePort UDP) must not
				// be dropped alongside TCP return traffic.
				DstPort: "32768-65535",
				Action:  "accept",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.wantRule.Name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.wantRule, ruleset.Rules[tc.idx])
		})
	}
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
	desiredRuleset := DefaultBareMetalIngressRuleset()

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
					// Verify the first deny rule is present in bracket notation.
					assert.Equal(t, "discard", r.PostFormValue("rules[input][0][action]"))
					assert.Equal(t, "22", r.PostFormValue("rules[input][0][dst_port]"))
					// Verify the 6443 DENY is present.
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
