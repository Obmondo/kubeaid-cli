// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package enrolment writes the per-tenant Wazuh agent enrolment bundle
// Secrets: the manager address and ports, a copy of the manager's authd
// password, the CA of the manager's certificate when known, and install
// scripts for Linux, Windows and macOS that verify the packages they
// download. A bundle is created when missing and updated when its
// rendered content differs; values are compared, never reported.
package enrolment

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

// Component is the report component name.
const Component = "enrolment"

// KindBundle is the report kind of a bundle Secret.
const KindBundle = "bundle"

// Bundle Secret keys.
const (
	KeyManagerHost      = "manager_host"
	KeyRegistrationPort = "registration_port"
	KeyEventsPort       = "events_port"
	KeyAuthdPass        = "authd.pass"
	KeyManagerCA        = "manager-ca.pem"
	KeyInstallLinux     = "install-linux.sh"
	KeyInstallWindows   = "install-windows.ps1"
	KeyInstallMacOS     = "install-macos.sh"
)

const (
	packagesURL = "https://packages.wazuh.com/4.x"
	// gpgKeyURL is the Wazuh package signing key; gpgKeyFingerprint is
	// its fingerprint, checked by the Linux script when gpg is there.
	gpgKeyURL         = "https://packages.wazuh.com/key/GPG-KEY-WAZUH"
	gpgKeyFingerprint = "0DCFCA5547B19D2A6099506096B3EE5F29111145"
	// macOSSigner is the Developer ID the macOS pkg is signed with
	// (pkgutil --check-signature); the team id does not change when the
	// certificate is renewed.
	macOSSigner = "Developer ID Installer: Wazuh Inc (KLZK8P68R5)"
	// windowsSigner must appear in the MSI's Authenticode signer subject.
	windowsSigner = "Wazuh, Inc"
)

// CA file the scripts write on the endpoint; the agent reads it at
// every enrolment (<enrollment><server_ca_path>), so it stays.
const (
	unixCAPath    = "/etc/wazuh-manager-ca.pem"
	windowsCAFile = "wazuh-manager-ca.pem" // in %ProgramData%
)

// Ensure creates or updates every bundle. A bundle whose authd Secret
// cannot be read is reported as an error and the others continue. A
// CA Secret that does not exist (yet) leaves the CA out of the bundle.
func Ensure(ctx context.Context, kube kubernetes.Interface, bundles []config.EnrolmentBundle, dryRun bool) []report.Result {
	store := secrets.Store{Kube: kube}
	results := make([]report.Result, 0, len(bundles))
	for _, b := range bundles {
		res := report.Result{Component: Component, Kind: KindBundle, Name: b.Namespace + "/" + b.Name}
		pass, err := store.ReadRaw(ctx, b.AuthdSecretRef)
		if err != nil {
			res.Action, res.Detail = report.ActionError, "authd password: "+err.Error()
			results = append(results, res)
			continue
		}
		var ca []byte
		note := ""
		if b.CASecretRef != nil {
			ca, err = store.ReadRaw(ctx, *b.CASecretRef)
			switch {
			case apierrors.IsNotFound(err):
				ca, note = nil, "without manager CA (Secret "+b.CASecretRef.Namespace+"/"+b.CASecretRef.Name+" not found)"
			case err != nil:
				res.Action, res.Detail = report.ActionError, "manager CA: "+err.Error()
				results = append(results, res)
				continue
			}
		}
		action, changed, err := secrets.Apply(ctx, kube, b.Namespace, b.Name, Render(b, pass, ca), dryRun)
		res.Action = action
		switch {
		case err != nil:
			res.Detail = err.Error()
		case action == report.ActionCreate:
			res.Detail = "tenant " + b.Tenant
		case action == report.ActionUpdate:
			res.Detail = "keys " + strings.Join(changed, ",")
		}
		if note != "" && err == nil {
			res.Detail = strings.TrimPrefix(res.Detail+"; "+note, "; ")
		}
		results = append(results, res)
	}
	return results
}

// Render returns the bundle Secret data. authdPass is copied as is
// into authd.pass; the scripts use it without a trailing newline.
// managerCA (PEM, optional) goes into manager-ca.pem, and the scripts
// then write it to the endpoint and pass it as WAZUH_REGISTRATION_CA,
// so the agent verifies the manager's certificate (and host name)
// when it enrols.
func Render(b config.EnrolmentBundle, authdPass, managerCA []byte) map[string][]byte {
	pass := strings.TrimRight(string(authdPass), "\r\n")
	ca := strings.TrimSpace(string(managerCA))
	env := [][2]string{
		{"WAZUH_MANAGER", b.ManagerHost},
		{"WAZUH_MANAGER_PORT", strconv.Itoa(b.EventsPort)},
		{"WAZUH_REGISTRATION_SERVER", b.ManagerHost},
		{"WAZUH_REGISTRATION_PORT", strconv.Itoa(b.RegistrationPort)},
		{"WAZUH_REGISTRATION_PASSWORD", pass},
	}
	out := map[string][]byte{
		KeyManagerHost:      []byte(b.ManagerHost),
		KeyRegistrationPort: []byte(strconv.Itoa(b.RegistrationPort)),
		KeyEventsPort:       []byte(strconv.Itoa(b.EventsPort)),
		KeyAuthdPass:        authdPass,
		KeyInstallLinux:     []byte(linuxScript(b, env, ca)),
		KeyInstallWindows:   []byte(windowsScript(b, env, ca)),
		KeyInstallMacOS:     []byte(macOSScript(b, env, ca)),
	}
	if ca != "" {
		out[KeyManagerCA] = []byte(ca + "\n")
	}
	return out
}

// checksumDir holds the published SHA-512 files (<package file>.sha512)
// of version v (e.g. 4.14.8-1, whose directory is 4.14.8).
func checksumDir(v string) string {
	return packagesURL + "/checksums/wazuh/" + strings.SplitN(v, "-", 2)[0]
}

// unixCA writes the CA to unixCAPath and exports WAZUH_REGISTRATION_CA.
func unixCA(ca string) string {
	if ca == "" {
		return ""
	}
	return fmt.Sprintf(`# The manager's CA: the agent verifies the manager's certificate when it enrols.
umask 022
cat > %[1]s <<'WAZUH_MANAGER_CA'
%[2]s
WAZUH_MANAGER_CA
export WAZUH_REGISTRATION_CA=%[1]s
`, unixCAPath, ca)
}

// linuxScript installs the deb from the Wazuh apt repository or the
// rpm with its signature checked, both against the Wazuh signing key;
// the package scripts read the WAZUH_* variables from the environment.
func linuxScript(b config.EnrolmentBundle, env [][2]string, ca string) string {
	var s strings.Builder
	fmt.Fprintf(&s, "#!/bin/sh\n# Wazuh agent %s for tenant %s. Run as root.\nset -eu\n", b.AgentVersion, b.Tenant)
	for _, kv := range env {
		fmt.Fprintf(&s, "export %s=%s\n", kv[0], shQuote(kv[1]))
	}
	s.WriteString(unixCA(ca))
	// apt-get/dnf install resolve the package's dependencies (lsb-release,
	// adduser on minimal images), which dpkg -i and rpm -i do not.
	fmt.Fprintf(&s, `V=%[1]s
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
# Packages are checked against the Wazuh signing key; its fingerprint is
# checked too where gpg can show it.
curl -fsSLo "$T/wazuh.asc" %[2]s
if command -v gpg >/dev/null 2>&1 && K=$(gpg --batch --with-colons --import-options show-only --import "$T/wazuh.asc" 2>/dev/null); then
  echo "$K" | grep -q '^fpr:*%[3]s:' || { echo "unexpected Wazuh signing key" >&2; exit 1; }
fi
if command -v dpkg >/dev/null 2>&1; then
  # apt verifies the repository signature; the source is removed again, so
  # no later upgrade takes the agent past the manager's version.
  install -d -m 0755 /usr/share/keyrings
  if command -v gpg >/dev/null 2>&1; then
    K=/usr/share/keyrings/wazuh.gpg
    gpg --batch --yes --dearmor -o "$K" "$T/wazuh.asc"
  else
    K=/usr/share/keyrings/wazuh.asc
    cp "$T/wazuh.asc" "$K"
  fi
  chmod 0644 "$K"
  L=/etc/apt/sources.list.d/wazuh-agent-install.list
  echo "deb [signed-by=$K] %[4]s/apt/ stable main" > "$L"
  trap 'rm -rf "$T" "$L"' EXIT
  apt-get update
  apt-get install -y "wazuh-agent=$V"
else
  F="wazuh-agent-${V}.$(uname -m).rpm"
  curl -fsSLo "$T/$F" "%[4]s/yum/$F"
  rpm --import "$T/wazuh.asc"
  S=$(rpm -K "$T/$F") || { echo "$S" >&2; echo "package signature check failed" >&2; exit 1; }
  case "$S" in
    *"NOT OK"*|*MISSING*|*NOKEY*) echo "$S" >&2; exit 1 ;;
    *"signatures OK"*|*pgp*|*gpg*|*PGP*|*GPG*) ;;
    *) echo "$S" >&2; echo "package is not signed" >&2; exit 1 ;;
  esac
  if command -v dnf >/dev/null 2>&1; then dnf install -y "$T/$F"; else yum install -y "$T/$F"; fi
fi
systemctl daemon-reload
systemctl enable --now wazuh-agent
`, b.AgentVersion, gpgKeyURL, gpgKeyFingerprint, packagesURL)
	return s.String()
}

// windowsScript installs the MSI with the WAZUH_* properties after
// checking its published SHA-512 and its Authenticode signature.
func windowsScript(b config.EnrolmentBundle, env [][2]string, ca string) string {
	args := []string{"'/i'", "('\"{0}\"' -f $msi)", "'/q'"}
	for _, kv := range env {
		args = append(args, psQuote(kv[0]+`="`+strings.ReplaceAll(kv[1], `"`, `""`)+`"`))
	}
	caBlock := ""
	if ca != "" {
		args = append(args, `('WAZUH_REGISTRATION_CA="{0}"' -f $ca)`)
		caBlock = fmt.Sprintf(`# The manager's CA: the agent verifies the manager's certificate when it enrols.
$ca = Join-Path $env:ProgramData '%s'
Set-Content -Path $ca -Encoding Ascii -Value @'
%s
'@
`, windowsCAFile, ca)
	}
	msi := "wazuh-agent-" + b.AgentVersion + ".msi"
	return fmt.Sprintf(`# Wazuh agent %[1]s for tenant %[2]s. Run in an elevated PowerShell.
$ErrorActionPreference = 'Stop'
$msi = Join-Path $env:TEMP '%[3]s'
$sumFile = "$msi.sha512"
Invoke-WebRequest -UseBasicParsing -Uri '%[4]s/windows/%[3]s' -OutFile $msi
Invoke-WebRequest -UseBasicParsing -Uri '%[5]s' -OutFile $sumFile
$sum = ((Get-Content -Raw $sumFile).Trim() -split '\s+')[0]
if ((Get-FileHash -Algorithm SHA512 $msi).Hash -ne $sum) { throw "checksum mismatch for $msi" }
$sig = Get-AuthenticodeSignature $msi
if ($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notlike '*%[6]s*') { throw "unexpected signature on ${msi}: $($sig.Status) $($sig.SignerCertificate.Subject)" }
%[7]s$p = Start-Process msiexec.exe -Wait -PassThru -ArgumentList @(%[8]s)
if ($p.ExitCode -ne 0) { throw "msiexec failed with exit code $($p.ExitCode)" }
Remove-Item $msi, $sumFile
NET START Wazuh
`, b.AgentVersion, b.Tenant, msi, packagesURL, checksumDir(b.AgentVersion)+"/"+msi+".sha512", windowsSigner, caBlock, strings.Join(args, ", "))
}

// macOSScript writes /tmp/wazuh_envs, which the pkg's install script
// sources, and installs the pkg for the host's architecture after
// checking its published SHA-512 and its Developer ID signature.
func macOSScript(b config.EnrolmentBundle, env [][2]string, ca string) string {
	if ca != "" {
		env = append(env, [2]string{"WAZUH_REGISTRATION_CA", unixCAPath})
	}
	lines := make([]string, 0, len(env))
	for _, kv := range env {
		lines = append(lines, shQuote(kv[0]+"="+shQuote(kv[1])))
	}
	caBlock := ""
	if ca != "" {
		caBlock = fmt.Sprintf(`# The manager's CA: the agent verifies the manager's certificate when it enrols.
umask 022
cat > %s <<'WAZUH_MANAGER_CA'
%s
WAZUH_MANAGER_CA
`, unixCAPath, ca)
	}
	return fmt.Sprintf(`#!/bin/sh
# Wazuh agent %[1]s for tenant %[2]s. Run as root.
set -eu
A=intel64
if [ "$(uname -m)" = arm64 ]; then A=arm64; fi
%[3]sF="wazuh-agent-%[1]s.$A.pkg"
T=$(mktemp -d)
trap 'rm -rf "$T" /tmp/wazuh_envs' EXIT
curl -fsSLo "$T/$F" "%[4]s/macos/$F"
curl -fsSLo "$T/$F.sha512" "%[5]s/$F.sha512"
[ "$(awk '{print $1}' "$T/$F.sha512")" = "$(shasum -a 512 "$T/$F" | awk '{print $1}')" ] || { echo "checksum mismatch for $F" >&2; exit 1; }
pkgutil --check-signature "$T/$F" | grep -qF '%[6]s' || { echo "$F is not signed by %[6]s" >&2; exit 1; }
umask 077
printf '%%s\n' %[7]s > /tmp/wazuh_envs
installer -pkg "$T/$F" -target /
launchctl bootstrap system /Library/LaunchDaemons/com.wazuh.agent.plist
`, b.AgentVersion, b.Tenant, caBlock, packagesURL, checksumDir(b.AgentVersion), macOSSigner, strings.Join(lines, " "))
}

// shQuote quotes s for POSIX sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
