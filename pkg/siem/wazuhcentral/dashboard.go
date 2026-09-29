// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuhcentral

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

const (
	// FirstHostID is the id the first wazuh.yml host must have: the
	// Wazuh dashboard image's start script looks for it.
	FirstHostID = "1513629884013"
	// DashboardConfigKey is the Secret key holding wazuh.yml.
	DashboardConfigKey = "wazuh.yml"

	defaultAPIPort = 55000
)

// DashboardHost is one manager API entry in wazuh.yml.
type DashboardHost struct {
	ID       string
	URL      string // scheme://host, without port
	Port     int
	Username string
	Password string
}

// RenderDashboardConfig renders the Wazuh dashboard app's wazuh.yml.
// Strings are emitted as JSON strings, which are valid YAML
// double-quoted scalars, so credentials need no further escaping.
func RenderDashboardConfig(hosts []DashboardHost) []byte {
	var b strings.Builder
	b.WriteString(`pattern: "*:wazuh-alerts-*"
checks.pattern: false
checks.template: false
checks.fields: false
checks.api: true
checks.setup: true
hosts:
`)
	if len(hosts) == 0 {
		b.WriteString("  []\n")
	}
	for _, h := range hosts {
		fmt.Fprintf(&b, "  - %s:\n", h.ID)
		fmt.Fprintf(&b, "      url: %s\n", quote(h.URL))
		fmt.Fprintf(&b, "      port: %d\n", h.Port)
		fmt.Fprintf(&b, "      username: %s\n", quote(h.Username))
		fmt.Fprintf(&b, "      password: %s\n", quote(h.Password))
		b.WriteString("      run_as: true\n")
	}
	return []byte(b.String())
}

func quote(s string) string {
	b, _ := json.Marshal(s) //nolint:errchkjson // a string always marshals
	return string(b)
}

// hostID is FirstHostID for the first manager and "t<code>" for the
// others.
func hostID(i int, tenant string) string {
	if i == 0 {
		return FirstHostID
	}
	return "t" + tenant
}

// splitAPIURL returns the URL without port and the port (default
// 55000).
func splitAPIURL(raw string) (string, int, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, fmt.Errorf("parsing manager url: %w", err)
	}
	if u.Scheme == "" || u.Hostname() == "" {
		return "", 0, fmt.Errorf("manager url %q needs scheme and host", raw)
	}
	port := defaultAPIPort
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return "", 0, fmt.Errorf("manager url %q: bad port", raw)
		}
	}
	host := u.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, port, nil
}

// EnsureDashboardConfig writes wazuh.yml, listing every manager with
// the API credentials read from its credSecretRef, to the target
// Secret. A manager whose credentials cannot be read is reported as an
// error and the Secret is left as it is, so a working dashboard never
// loses a host because of one unreadable Secret.
func EnsureDashboardConfig(ctx context.Context, kube kubernetes.Interface, target config.ObjectRef, managers []config.Wazuh, dryRun bool) []report.Result {
	var results []report.Result
	add := func(kind, name string, a report.Action, detail string) {
		results = append(results, report.Result{Component: Component, Kind: kind, Name: name, Action: a, Detail: detail})
	}
	store := secrets.Store{Kube: kube}
	secretName := target.Namespace + "/" + target.Name

	hosts := make([]DashboardHost, 0, len(managers))
	var missing []string
	for i, m := range managers {
		h, err := dashboardHost(ctx, store, m)
		if err != nil {
			add("dashboard-host", m.Tenant, report.ActionError, err.Error())
			missing = append(missing, m.Tenant)
			continue
		}
		h.ID = hostID(i, m.Tenant)
		hosts = append(hosts, h)
	}
	if len(missing) > 0 {
		add("dashboard-config", secretName, report.ActionSkip,
			"not written: no credentials for tenant(s) "+strings.Join(missing, ","))
		return results
	}

	data := map[string][]byte{DashboardConfigKey: RenderDashboardConfig(hosts)}
	a, _, err := secrets.Apply(ctx, kube, target.Namespace, target.Name, data, dryRun)
	detail := fmt.Sprintf("%d hosts", len(hosts))
	if err != nil {
		detail = err.Error()
	}
	add("dashboard-config", secretName, a, detail)
	return results
}

func dashboardHost(ctx context.Context, store secrets.Store, m config.Wazuh) (DashboardHost, error) {
	u, port, err := splitAPIURL(m.URL)
	if err != nil {
		return DashboardHost{}, err
	}
	ref := m.CredSecretRef
	user, err := store.Read(ctx, config.SecretRef{Namespace: ref.Namespace, Name: ref.Name, Key: ref.UsernameKey})
	if err != nil {
		return DashboardHost{}, err
	}
	pass, err := store.Read(ctx, config.SecretRef{Namespace: ref.Namespace, Name: ref.Name, Key: ref.PasswordKey})
	if err != nil {
		return DashboardHost{}, err
	}
	return DashboardHost{URL: u, Port: port, Username: user, Password: pass}, nil
}
