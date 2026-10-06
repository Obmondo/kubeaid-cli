# How KubeAid CLI uses Cluster API

KubeAid CLI uses Cluster API to create and manage AWS, Azure, and Hetzner
clusters. During bootstrap, it uses the upstream `clusterctl` Go client to run
`clusterctl move` and transfer management to the target cluster.

The CLI also handles work that Cluster API does not provide: generating KubeAid
configuration, preparing provider infrastructure, rendering a KubeAid Config
repository, bootstrapping the platform, and provisioning generic SSH-only bare
metal through KubeOne. That is why it is a separate tool.

## Responsibilities

| Layer | What it owns |
| --- | --- |
| Cluster API | Reconciliation of `Cluster`, control-plane, `MachineDeployment`, `MachinePool`, and provider-specific resources. |
| `clusterctl` | Management-cluster setup, provider lifecycle operations, CAPI template and configuration operations, and moving CAPI resources between management clusters. |
| KubeAid CLI | Configuration generation, prerequisite infrastructure, bootstrap sequencing, GitOps repository rendering, and the KubeAid platform installation. |

This is reflected in the code:

- `BootstrapCluster` creates the management environment, provisions the target,
  configures the target, and syncs the remaining Argo CD applications.
- `CreateDevEnv` clones `kubeaid-config`, creates the local K3D management
  cluster for CAPI providers, and runs the KubeAid CLI setup for that cluster.
- `pivotCluster` waits until all CAPI machines are running, creates the
  upstream `clusterctl` client, and calls `Move`.

KubeAid CLI does not reimplement CAPI reconciliation or `clusterctl move`.

## The K3D pivot

For Cluster API providers, bootstrap works like this:

1. Prepare any prerequisites that CAPI cannot create itself. The Hetzner path
   includes network, SSH, NAT, VSwitch, Robot operating-system installation,
   storage-plan, and control-plane load-balancer work.
2. Clone the user's KubeAid Config repository and create a local K3D
   management cluster.
3. Install the Cluster API core and provider controllers through KubeAid CLI's
   management-cluster setup.
4. Render the CAPI and Argo CD configuration into the KubeAid Config
   repository.
5. Let the CAPI provider provision the target cluster.
6. Set up the target cluster, including the Argo CD applications needed before
   the pivot.
7. Call `clusterctl move` to transfer the CAPI resources to the target
   cluster. The target then runs the CAPI controllers that manage those
   resources.

After the move, the target runs the CAPI controllers. The local K3D management
cluster can be removed.

## Why this stays outside `clusterctl`

`clusterctl` needs to work for Cluster API users in general. KubeAid CLI makes
choices that `clusterctl` does not make:

| KubeAid CLI behavior | Why KubeAid CLI owns it |
| --- | --- |
| `general.yaml`, `secrets.yaml`, prompts, defaults, validation, and resume behavior | This is KubeAid CLI's configuration API. |
| Hetzner networks, NAT, VSwitches, Robot provisioning, and load-balancer preparation | Another CAPH user may use Terraform, Crossplane, existing network infrastructure, or a different bootstrap design. |
| KubeAid Config repository cloning, rendering, commits, pushes, and Git authentication | Cluster API does not require Git or prescribe a repository layout. |
| Argo CD, Sealed Secrets, Cilium, cert-manager, monitoring, storage, backup, and application sync order | These are KubeAid platform components. Other CAPI users may use Flux, another CNI, different secret handling, or no platform layer. |
| KubeOne for generic SSH-only bare metal | This path is intentionally outside Cluster API. |

Putting this in `clusterctl` would make it a KubeAid CLI installer. Cluster API
maintainers would then have to support KubeAid CLI's chart selection, GitOps
layout, provider setup, and compatibility rules.

An extension would not change that. KubeAid CLI would still maintain the
extension, configuration schema, templates, and tests. It makes sense only if
it also solves a problem for Cluster API users who do not use KubeAid CLI.

## Worker capacity is configured by KubeAid CLI

The HCloud worker-group regression shows this boundary.

A pure HCloud configuration could render a control plane with no HCloud worker
group. Cluster API reconciled what it was given. With no worker
`MachineDeployment` to create, the cluster had no declared worker capacity.

The required changes were in KubeAid CLI:

- a new pure-HCloud configuration adds a default worker group only when no
  HCloud group is configured;
- a hybrid configuration still has no default HCloud worker group because its
  workers are bare metal; and
- a resumed configuration restores every saved HCloud worker group before it
  is rendered again.

Prompt and render tests cover the HCloud worker configuration, hybrid
configuration, and restoring saved HCloud worker groups. Cluster API still owns
the later step: reconciling the rendered `MachineDeployment` resources.

## Upstream work

KubeAid CLI configuration, chart selection, and bootstrap flow do not belong
in `clusterctl`. A change belongs upstream when it is useful without KubeAid
CLI:

- defects or unclear behavior in Cluster API, `clusterctl`, or a provider;
- provider validation, readiness, lifecycle, or move behavior with a
general-purpose use case;
- provider templates that do not depend on KubeAid CLI configuration or chart
  stack; and
- documentation for the disposable management-cluster pattern where it applies
  to more than KubeAid CLI.

If a change makes sense for an operator without a KubeAid Config repository or
the KubeAid platform, it is an upstream candidate. If it depends on KubeAid CLI
configuration, GitOps layout, or chart composition, it belongs in KubeAid CLI.

## Source references

- [`pkg/core/bootstrap_cluster.go`](../pkg/core/bootstrap_cluster.go)
- [`pkg/core/create_dev_env.go`](../pkg/core/create_dev_env.go)
- [`pkg/core/setup_cluster.go`](../pkg/core/setup_cluster.go)
- [`pkg/config/prompt/provider_hetzner.go`](../pkg/config/prompt/provider_hetzner.go)
- [`pkg/config/prompt/resume.go`](../pkg/config/prompt/resume.go)
- [`pkg/config/prompt/resume_test.go`](../pkg/config/prompt/resume_test.go)
- [Cluster API: `clusterctl init`](https://cluster-api.sigs.k8s.io/reference/clusterctl/commands/init)
- [Cluster API: `clusterctl move`](https://cluster-api.sigs.k8s.io/reference/clusterctl/commands/move)
