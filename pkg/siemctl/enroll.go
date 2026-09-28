// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// EnrolOS is an agent operating system.
type EnrolOS string

// Supported operating systems.
const (
	OSLinux   EnrolOS = "linux"
	OSMacOS   EnrolOS = "macos"
	OSWindows EnrolOS = "windows"
)

// installKey maps an OS to its install script in the bundle.
var installKey = map[EnrolOS]string{
	OSLinux:   BundleKeyInstallLinux,
	OSMacOS:   BundleKeyInstallMacOS,
	OSWindows: BundleKeyInstallWindows,
}

// ParseEnrolOS validates an --os value.
func ParseEnrolOS(s string) (EnrolOS, error) {
	o := EnrolOS(strings.ToLower(s))
	if _, ok := installKey[o]; !ok {
		return "", fmt.Errorf("unknown --os %q (want linux, macos or windows)", s)
	}
	return o, nil
}

// EnrolmentBundle is a tenant's bundle, read from the cluster.
type EnrolmentBundle struct {
	Tenant string
	Data   map[string][]byte
}

// ReadEnrolmentBundle reads the enrolment-bundle Secret of a tenant.
func ReadEnrolmentBundle(ctx context.Context, kube kubernetes.Interface, tenant string) (*EnrolmentBundle, error) {
	ns := constants.SecurityOperationsTenantNamespacePrefix + tenant
	secret, err := kube.CoreV1().Secrets(ns).Get(ctx, EnrolmentBundleSecret, metaV1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading Secret %s/%s (has the reconciler run with dry-run off?): %w",
			ns, EnrolmentBundleSecret, err)
	}
	return &EnrolmentBundle{Tenant: tenant, Data: secret.Data}, nil
}

// WriteInstructions prints how to enrol a host of the given OS: the agent
// install script (it carries the tenant's enrolment password, so treat the
// output as a secret) and the Velociraptor client hint.
func (b *EnrolmentBundle) WriteInstructions(w io.Writer, target EnrolOS) error {
	key := installKey[target]
	script, ok := b.Data[key]
	if !ok {
		return fmt.Errorf("the enrolment bundle of tenant %s has no %s", b.Tenant, key)
	}
	var s strings.Builder
	_, _ = fmt.Fprintf(&s, "# Tenant %s: Wazuh manager %s, registration port %s, events port %s\n",
		b.Tenant, b.Data[BundleKeyManagerHost], b.Data[BundleKeyRegistrationPort], b.Data[BundleKeyEventsPort])
	switch target {
	case OSLinux, OSMacOS:
		_, _ = fmt.Fprintf(&s, "# Save as %s and run it as root:\n#   sudo sh ./%s\n", key, key)
	case OSWindows:
		_, _ = fmt.Fprintf(&s, "# Save as %s and run it in an elevated PowerShell:\n#   powershell -ExecutionPolicy Bypass -File .\\%s\n", key, key)
	}
	s.WriteString("# It contains the tenant's enrolment password: do not paste it into tickets or chat.\n\n")
	s.Write(script)
	if !strings.HasSuffix(string(script), "\n") {
		s.WriteString("\n")
	}
	if hint, ok := b.Data[BundleKeyVelociraptorInstall]; ok {
		s.WriteString("\n# ---- Velociraptor client ----\n")
		for _, line := range strings.Split(strings.TrimRight(string(hint), "\n"), "\n") {
			s.WriteString("# " + line + "\n")
		}
		_, _ = fmt.Fprintf(&s, "# Write the client config with: kubeaid-cli siem enroll %s --os %s --output-dir <dir>\n", b.Tenant, target)
	}
	_, err := io.WriteString(w, s.String())
	return err
}

// WriteFiles writes the OS's install script and the Velociraptor client
// config into dir (0600). Returns the written paths.
func (b *EnrolmentBundle) WriteFiles(dir string, target EnrolOS) ([]string, error) {
	if err := mkdirAll(dir); err != nil {
		return nil, err
	}
	var written []string
	for _, key := range []string{installKey[target], BundleKeyVelociraptorConfig, BundleKeyVelociraptorInstall} {
		data, ok := b.Data[key]
		if !ok {
			continue
		}
		p := filepath.Join(dir, key)
		if err := writeFile(p, data, 0o600); err != nil {
			return written, err
		}
		written = append(written, p)
	}
	return written, nil
}

func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o700) }

func writeFile(p string, data []byte, mode os.FileMode) error { return os.WriteFile(p, data, mode) }
