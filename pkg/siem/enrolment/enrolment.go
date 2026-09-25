// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package enrolment writes the per-tenant Wazuh agent enrolment bundle
// Secrets: the manager address and ports, a copy of the manager's authd
// password and install scripts for Linux, Windows and macOS. A bundle
// is created when missing and updated when its rendered content
// differs; values are compared, never reported.
package enrolment

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

// Component is the report component name.
const Component = "enrolment"

// Bundle Secret keys.
const (
	KeyManagerHost      = "manager_host"
	KeyRegistrationPort = "registration_port"
	KeyEventsPort       = "events_port"
	KeyAuthdPass        = "authd.pass"
	KeyInstallLinux     = "install-linux.sh"
	KeyInstallWindows   = "install-windows.ps1"
	KeyInstallMacOS     = "install-macos.sh"
)

const packagesURL = "https://packages.wazuh.com/4.x"

// Ensure creates or updates every bundle. A bundle whose authd Secret
// cannot be read is reported as an error and the others continue.
func Ensure(ctx context.Context, kube kubernetes.Interface, bundles []config.EnrolmentBundle, dryRun bool) []report.Result {
	store := secrets.Store{Kube: kube}
	results := make([]report.Result, 0, len(bundles))
	for _, b := range bundles {
		res := report.Result{Component: Component, Kind: "bundle", Name: b.Namespace + "/" + b.Name}
		pass, err := store.ReadRaw(ctx, b.AuthdSecretRef)
		if err != nil {
			res.Action, res.Detail = report.ActionError, "authd password: "+err.Error()
			results = append(results, res)
			continue
		}
		action, changed, err := secrets.Apply(ctx, kube, b.Namespace, b.Name, Render(b, pass), dryRun)
		res.Action = action
		switch {
		case err != nil:
			res.Detail = err.Error()
		case action == report.ActionCreate:
			res.Detail = "tenant " + b.Tenant
		case action == report.ActionUpdate:
			res.Detail = "keys " + strings.Join(changed, ",")
		}
		results = append(results, res)
	}
	return results
}

// Render returns the bundle Secret data. authdPass is copied as is
// into authd.pass; the scripts use it without a trailing newline.
func Render(b config.EnrolmentBundle, authdPass []byte) map[string][]byte {
	pass := strings.TrimRight(string(authdPass), "\r\n")
	env := [][2]string{
		{"WAZUH_MANAGER", b.ManagerHost},
		{"WAZUH_MANAGER_PORT", strconv.Itoa(b.EventsPort)},
		{"WAZUH_REGISTRATION_SERVER", b.ManagerHost},
		{"WAZUH_REGISTRATION_PORT", strconv.Itoa(b.RegistrationPort)},
		{"WAZUH_REGISTRATION_PASSWORD", pass},
	}
	return map[string][]byte{
		KeyManagerHost:      []byte(b.ManagerHost),
		KeyRegistrationPort: []byte(strconv.Itoa(b.RegistrationPort)),
		KeyEventsPort:       []byte(strconv.Itoa(b.EventsPort)),
		KeyAuthdPass:        authdPass,
		KeyInstallLinux:     []byte(linuxScript(b, env)),
		KeyInstallWindows:   []byte(windowsScript(b, env)),
		KeyInstallMacOS:     []byte(macOSScript(b, env)),
	}
}

// linuxScript installs the deb or rpm package; the package scripts
// read the WAZUH_* variables from the environment.
func linuxScript(b config.EnrolmentBundle, env [][2]string) string {
	var s strings.Builder
	fmt.Fprintf(&s, "#!/bin/sh\n# Wazuh agent %s for tenant %s. Run as root.\nset -eu\n", b.AgentVersion, b.Tenant)
	for _, kv := range env {
		fmt.Fprintf(&s, "export %s=%s\n", kv[0], shQuote(kv[1]))
	}
	fmt.Fprintf(&s, `V=%s
if command -v dpkg >/dev/null 2>&1; then
  F="wazuh-agent_${V}_$(dpkg --print-architecture).deb"
  curl -fsSLo "/tmp/$F" "%s/apt/pool/main/w/wazuh-agent/$F"
  dpkg -i "/tmp/$F"
else
  F="wazuh-agent-${V}.$(uname -m).rpm"
  curl -fsSLo "/tmp/$F" "%s/yum/$F"
  rpm -ihv "/tmp/$F"
fi
rm -f "/tmp/$F"
systemctl daemon-reload
systemctl enable --now wazuh-agent
`, b.AgentVersion, packagesURL, packagesURL)
	return s.String()
}

// windowsScript installs the MSI with the WAZUH_* properties.
func windowsScript(b config.EnrolmentBundle, env [][2]string) string {
	args := []string{"'/i'", "('\"{0}\"' -f $msi)", "'/q'"}
	for _, kv := range env {
		args = append(args, psQuote(kv[0]+`="`+strings.ReplaceAll(kv[1], `"`, `""`)+`"`))
	}
	msi := "wazuh-agent-" + b.AgentVersion + ".msi"
	return fmt.Sprintf(`# Wazuh agent %s for tenant %s. Run in an elevated PowerShell.
$ErrorActionPreference = 'Stop'
$msi = Join-Path $env:TEMP '%s'
Invoke-WebRequest -UseBasicParsing -Uri '%s/windows/%s' -OutFile $msi
$p = Start-Process msiexec.exe -Wait -PassThru -ArgumentList @(%s)
if ($p.ExitCode -ne 0) { throw "msiexec failed with exit code $($p.ExitCode)" }
Remove-Item $msi
NET START Wazuh
`, b.AgentVersion, b.Tenant, msi, packagesURL, msi, strings.Join(args, ", "))
}

// macOSScript writes /tmp/wazuh_envs, which the pkg's install script
// sources, and installs the pkg for the host's architecture.
func macOSScript(b config.EnrolmentBundle, env [][2]string) string {
	lines := make([]string, 0, len(env))
	for _, kv := range env {
		lines = append(lines, shQuote(kv[0]+"="+shQuote(kv[1])))
	}
	return fmt.Sprintf(`#!/bin/sh
# Wazuh agent %s for tenant %s. Run as root.
set -eu
A=intel64
if [ "$(uname -m)" = arm64 ]; then A=arm64; fi
umask 077
printf '%%s\n' %s > /tmp/wazuh_envs
curl -fsSLo /tmp/wazuh-agent.pkg "%s/macos/wazuh-agent-%s.$A.pkg"
installer -pkg /tmp/wazuh-agent.pkg -target /
rm -f /tmp/wazuh-agent.pkg /tmp/wazuh_envs
launchctl bootstrap system /Library/LaunchDaemons/com.wazuh.agent.plist
`, b.AgentVersion, b.Tenant, strings.Join(lines, " "), packagesURL, b.AgentVersion)
}

// shQuote quotes s for POSIX sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
