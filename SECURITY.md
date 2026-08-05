# Security Policy

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report it privately via
[GitHub Security Advisories](https://github.com/Obmondo/kubeaid-cli/security/advisories/new)
for this repository. A maintainer (see [MAINTAINERS.md](MAINTAINERS.md)) will
acknowledge the report, work with you to understand and validate the impact,
and coordinate a fix and disclosure timeline before any public advisory goes
out.

## Supported Versions

KubeAid CLI ships releases from `main` only (see [docs/release.md](docs/release.md));
there are no long-term-support branches. Security fixes land as a new patch
release off the latest minor version — please upgrade to the latest release
before reporting an issue that may already be fixed.

## Scope

This policy covers the `kubeaid-cli` and `kubeaid-storagectl` binaries and
their direct dependencies as pinned in `go.mod`. Vulnerabilities in the
Kubernetes clusters, cloud provider APIs, or Argo CD itself that this tool
orchestrates should be reported to those projects directly.
