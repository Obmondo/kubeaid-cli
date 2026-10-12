// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package enrolment

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

// Velociraptor keys of the bundle Secret. They are written by the
// velociraptor component, which knows the tenant's org.
const (
	KeyVelociraptorConfig  = "velociraptor-client.config.yaml"
	KeyVelociraptorInstall = "install-velociraptor.txt"
)

// velociraptorComponent is the report component of the Velociraptor
// keys: they are written in the velociraptor step (after the org
// exists), so `--only velociraptor` selects them.
const velociraptorComponent = "velociraptor"

const velociraptorReleases = "https://github.com/Velocidex/velociraptor/releases"

// VelociraptorBundle is a bundle plus the client configs of the orgs
// named like the tenant (exactly one is expected).
type VelociraptorBundle struct {
	Bundle        config.EnrolmentBundle
	Org           string
	ClientConfigs []string
}

// EnsureVelociraptor adds the tenant org's Velociraptor client config
// and an install hint to each bundle: created when the bundle is
// missing, updated when either key differs. Other keys are kept.
func EnsureVelociraptor(ctx context.Context, kube kubernetes.Interface, bundles []VelociraptorBundle, dryRun bool) []report.Result {
	results := make([]report.Result, 0, len(bundles))
	for _, vb := range bundles {
		b := vb.Bundle
		res := report.Result{Component: velociraptorComponent, Kind: KindBundle, Name: b.Namespace + "/" + b.Name}
		switch n := len(vb.ClientConfigs); {
		case n > 1:
			res.Action, res.Detail = report.ActionError, "more than one org named "+vb.Org
			results = append(results, res)
			continue
		case n == 0 && dryRun:
			// The org is created in this run's velociraptor step.
			res.Action, res.Detail = report.ActionCreate, "org "+vb.Org+" not created yet"
			results = append(results, res)
			continue
		case n == 0:
			res.Action, res.Detail = report.ActionError, "no client config for org "+vb.Org
			results = append(results, res)
			continue
		}
		data := RenderVelociraptor(b, vb.Org, vb.ClientConfigs[0])
		action, changed, err := secrets.Apply(ctx, kube, b.Namespace, b.Name, data, dryRun)
		res.Action = action
		switch {
		case err != nil:
			res.Detail = err.Error()
		case action == report.ActionCreate:
			res.Detail = "org " + vb.Org
		case action == report.ActionUpdate:
			res.Detail = "keys " + strings.Join(changed, ",")
		}
		results = append(results, res)
	}
	return results
}

// RenderVelociraptor returns the Velociraptor bundle keys: the client
// config as the server rendered it and a short install hint.
func RenderVelociraptor(b config.EnrolmentBundle, org, clientConfig string) map[string][]byte {
	return map[string][]byte{
		KeyVelociraptorConfig:  []byte(clientConfig),
		KeyVelociraptorInstall: []byte(velociraptorHint(b, org, clientVersion(clientConfig))),
	}
}

// clientVersion reads version.version from a client config.
func clientVersion(clientConfig string) string {
	var c struct {
		Version struct {
			Version string `yaml:"version"`
		} `yaml:"version"`
	}
	if err := yaml.Unmarshal([]byte(clientConfig), &c); err != nil {
		return ""
	}
	return c.Version.Version
}

// velociraptorHint tells how to turn the client config into an
// installed service with the release binary of the server's version.
func velociraptorHint(b config.EnrolmentBundle, org, version string) string {
	release, v := velociraptorReleases, "<version>"
	if version != "" {
		release, v = velociraptorReleases+"/tag/v"+version, version
	}
	return fmt.Sprintf(`Velociraptor client for tenant %s (org %s).

1. Download the binary for the host from
   %s
   (velociraptor-v%s-<os>-<arch>) and save %s
   next to it as client.config.yaml.
2. Install it as a service:
   Linux (deb, as root):
     ./velociraptor --config client.config.yaml debian client
     apt-get install -y ./velociraptor_client_*.deb
   Linux (rpm, as root):
     ./velociraptor --config client.config.yaml rpm client
     dnf install -y ./velociraptor_client_*.rpm
   Windows (elevated PowerShell):
     .\velociraptor.exe --config client.config.yaml service install
   macOS (as root):
     ./velociraptor --config client.config.yaml service install
`, b.Tenant, org, release, v, KeyVelociraptorConfig)
}
