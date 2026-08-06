# Roadmap

High-level direction for KubeAid CLI. For the detailed, engineering-level
write-up behind each item — problem statement, options considered, and the
chosen plan — see [`docs/TODO.md`](docs/TODO.md).

This roadmap reflects current priorities and will shift as the project and
its users grow; it isn't a committed release schedule.

## Day-2 cluster operations

- **Extend `cluster sync` beyond bare metal** — today, reconciling a change
  like an instance-type edit onto a *running* cloud cluster (AWS, Azure,
  Hetzner) has no supported day-2 path; only the bare-metal (KubeOne)
  provider supports `cluster sync`. Bringing the same inline-diff,
  double-approval, watch-until-rolled workflow to Cluster API–backed clouds
  is the largest open piece of work.
- **Pre-flight validation of rendered Helm values** — validate
  `values-capi-cluster.yaml` against the target chart's schema
  (`helm template --validate` / `kubeconform`) before pushing a config PR,
  so a schema violation surfaces as a clean, field-level error from
  kubeaid-cli instead of a failed ArgoCD sync discovered later.

## Multi-cloud and bare metal robustness

- **Cluster bootstrap resilience fixes** — a handful of edge cases in the
  bootstrap sequence (Cilium/CoreDNS name resolution once a control-plane
  load balancer's public interface is disabled, dev-build version strings
  leaking into rendered chart values) are being hardened as they're found.

## NetBird mesh networking

KubeAid CLI provisions and wires up NetBird-based mesh networking for
cluster access. Remaining automation work:

- Automatic DNS zone creation via the NetBird Management API, removing the
  last manual dashboard step in the provisioning flow.
- Long-lived PAT rotation for the NetBird operator's Management API token
  (current tokens cap at 180 days).
- Defaulting the NetBird operator's webhook to fail open
  (`failurePolicy: Ignore`) so a single unhealthy operator can't block all
  pod scheduling cluster-wide.

## Contributing to the roadmap

Have a use case this doesn't cover, or want to work on one of the items
above? Open an [issue](https://github.com/Obmondo/kubeaid-cli/issues) — see
[CONTRIBUTING.md](CONTRIBUTING.md) for how to get started.
