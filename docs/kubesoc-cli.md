# kubesoc: the `kubeaid-cli siem` commands

`kubeaid-cli siem` manages the security operations stack (kubesoc) of a KubeAid
managed cluster: the KubeAid `security-operations` chart (central Wazuh search,
DFIR-IRIS, MISP, Velociraptor, Ollama, the siem-reconciler) and one `wazuh`
release per tenant. The configuration is `cluster.securityOperations` in the
cluster's general config (see [security-operations.md](security-operations.md)
for the fields); every command reads it from `kubeaid-cli.general.yaml` in
`--cluster-dir` (a local checkout of `k8s/<cluster>` in the kubeaid-config
repository) unless `--general-config` says otherwise. No command needs
`secrets.yaml` or cloud credentials.

Cluster access is `$KUBECONFIG` (else `~/.kube/config`). Read-only commands:
`status`, `preflight`, `enroll`, and every `--dry-run`. Commands that change a
cluster ask first (or take `--yes`); destructive ones also need `--confirm`.

| Command | What it does |
|---|---|
| `siem init` | write or update `cluster.securityOperations` |
| `siem render` | render only the SOC files into `--cluster-dir`, no git |
| `siem preflight` | check the cluster is ready (read-only) |
| `siem apply` | preflight, render, commit on a branch, push, sync in order, wait, status |
| `siem status` | Applications, pods, reconciler, per-tenant health (read-only) |
| `siem upgrade` | new chart revision / reconciler tag: backup, apply, verify |
| `siem tenant add\|remove` | onboard or offboard a tenant |
| `siem backup` / `siem restore` | backup set of Velero, CNPG, MariaDB and OpenSearch snapshots |
| `siem enroll <tenant>` | print a tenant's agent install commands |
| `siem quickstart` | single-node evaluation install with Helm |
| `siem bundle` / `siem bundle import` | air-gapped bundle of images, charts, content, models |

## Typical flow

```sh
kubeaid-cli siem init --cluster-dir k8s/prod --domain soc.example.com \
  --keycloak-url https://keycloak.example.com/auth \
  --agent-host agents.example.com --agent-address 192.0.2.10 \
  --reconciler --tenant 001="Tenant A"
kubeaid-cli siem preflight --cluster-dir k8s/prod
kubeaid-cli siem apply --cluster-dir k8s/prod --dry-run          # the diff
kubeaid-cli siem apply --cluster-dir k8s/prod --push --sync      # commit, PR, merge, sync
kubeaid-cli siem status --cluster-dir k8s/prod
kubeaid-cli siem enroll 001 --os linux
```

## `siem init`

Creates or updates the block. Flags set fields; on a terminal, required fields
still empty are prompted for (`--non-interactive` fails instead). The rest of
the file, comments included, is kept; the block itself is rewritten (comments
inside it are lost, zero values are left out, `dryRun: false` is kept).
A missing file is created with `forkURLs` and `cluster.name`.

Flags: `--cluster-name`, `--kubeaid-url`, `--kubeaid-config-url`, `--domain`,
`--host-prefix`, `--keycloak-url`, `--keycloak-realm`, `--agent-host`,
`--agent-address`, `--chart-revision`, `--ingress-class`, `--cluster-issuer`,
`--shared-storage-class`, `--reconciler`, `--reconciler-tag`,
`--reconciler-dry-run`, `--ai-triage`, `--tenant code=Name` (repeatable),
`--non-interactive`, `--dry-run` (print the file instead of writing it).

## `siem render`

Unchanged: renders the Applications, values files and sealed Wazuh
credentials into `--cluster-dir`, keeping every existing credential. See
`kubeaid-cli siem render --help`.

## `siem preflight`

Read-only checks; `siem apply` and `siem upgrade` run them first
(`--skip-preflight` to skip):

- the cluster is reachable;
- CRDs and a running controller pod of Argo CD, sealed-secrets, cert-manager,
  CloudNativePG and mariadb-operator (a CRD without a running pod is a warning);
  Velero is a warning when missing, a failure with `--require-velero`;
- the ClusterIssuer (`clusterIssuer`) exists;
- a default StorageClass exists, and `sharedStorageClass` (RWX) when set;
- every host name resolves: central hosts, tenant dashboards, `agentHost`,
  Keycloak (a warning: external-dns may create them after the first sync;
  `--skip-dns` skips);
- the schedulable (untainted) nodes' allocatable CPU and memory roughly fit
  `--profile` (`standard`: 8 CPU / 24 GiB central + 2 CPU / 6 GiB per tenant
  indexer replica + 4 CPU / 10 GiB with AI triage; `single`: 3/8 + 1/3 + 2/8).

Exit code 1 on a failure, or a warning with `--strict`.

## `siem apply`

1. preflight;
2. `--dry-run`: renders into a scratch copy and prints the unified diff, then
   stops (no file, git or cluster change);
3. refuses when the cluster directory has uncommitted changes other than the
   general config (`--allow-dirty` to include them);
4. checks out `--branch` (default `kubesoc/apply-<UTC time>`, created from
   HEAD; `--current-branch` commits where you are), renders, and commits
   exactly the cluster directory with your own git (signing, hooks and SSH
   agent apply; `--message` sets the message);
5. `--push`: pushes the branch (never forced) and prints the pull request link
   against `--base` (default `main`);
6. `--sync`: when Argo CD reads another revision than the branch
   (`configRevision`, default the default branch) it waits for you to merge;
   then asks, and syncs through the Argo CD API in order: `root`, the
   Applications of the lowest `kubeaid.io/sync-order` (security-operations,
   which creates the tenant namespaces), `sealed-secrets`, then every other
   rendered Application (the `wazuh-<code>` ones). Each must become Synced and
   Healthy within `--timeout` (default 20m). The order is read from the labels
   of what the render wrote, so it follows the render;
7. prints `siem status` and fails unless everything is healthy.

## `siem status`

Read-only. Prints:

- Argo CD Applications `root`, `sealed-secrets`, `security-operations`,
  `wazuh-<code>`: sync, health, revision;
- pods per component and namespace (ready/total; finished Job pods excluded);
- the reconciler: its `/status` JSON when it runs long-running (Service
  `siem-reconciler-metrics`, port `metrics`, read through the API server's
  service proxy - no port-forward), including the detection content package
  version when it reports one; otherwise the last siem-reconciler Job (label
  `app.kubernetes.io/component=reconciler`): result, finish time and its
  `N changes` line from the pod log;
- per tenant: indexer StatefulSet readiness, whether the `enrolment-bundle`
  Secret has every key (key names only, values are never read out), and the
  connected agents (`agent_control -l` in `wazuh-<code>-manager-master-0`
  through pods/exec; `--agents=false` skips it).

`--strict` exits 1 unless everything is Synced, Healthy and ready.

## `siem upgrade`

```sh
kubeaid-cli siem upgrade --cluster-dir k8s/prod --chart-revision v33.0.0 \
  --reconciler-tag v1.3.0 --push --sync
```

Sets `chartRevision` and/or `reconciler.imageTag`, takes a backup (`siem backup
--wait`; `--skip-backup`, `--skip-velero`, `--velero-schedule`), then runs
`siem apply` with the same flags and verifies the status. The render keeps
every credential. `--dry-run` prints the render diff of the new revision.

## `siem tenant add <code>` / `siem tenant remove <code>`

`add` (`--name` required; `--registration-port`/`--events-port` for a code that
is not all digits, `--retention-days`, `--indexer-replicas`) adds the tenant to
the general config and renders its Application and fresh credentials; every
other tenant keeps its own. Then `siem apply`.

`remove` needs `--confirm`. It removes the tenant from the config, deletes
`sealed-secrets/wazuh-<code>/` and renders. `--export` first takes a Velero
Backup of the tenant namespace (`kubesoc-tenant-<code>-<time>`) and waits for
it. `--delete-namespace` also deletes the `wazuh-<code>` Application and
namespace now (asks again, or `--yes`). The IRIS customer and Velociraptor org
stay: the reconciler has no removal flags yet, remove them by hand.

Both take `--dry-run` (config and render diff only).

## `siem backup` / `siem restore <backup-set>`

`backup` creates one backup set, every object labelled
`kubesoc.io/backup-set=<name>` (default `kubesoc-<UTC time>`, `--name`):

- a Velero Backup of `security-operations` and every `wazuh-<code>` namespace,
  from the template of the Velero Schedule covering `security-operations`
  (or `--velero-schedule`), else a plain Backup with a 30 day TTL
  (`--skip-velero` leaves it out);
- a CloudNativePG Backup per PostgreSQL Cluster with backups configured
  (method and target copied from its ScheduledBackup);
- a MariaDB Backup per MariaDB, copying the storage of its scheduled Backup;
- one Job from each CronJob labelled `kubesoc.io/backup=opensearch-snapshot`.

`--dry-run` lists the objects; `--wait` waits for the Velero Backup
(`--timeout`, default 2h). Nothing found for a kind is reported as a note.

`restore` needs `--confirm` (and asks, or `--yes`): a Velero Restore of the
set's Backup (`--namespace` limits it; existing objects are left alone) and a
MariaDB Restore per MariaDB Backup of the set. CloudNativePG restores into a
new Cluster only (`bootstrap.recovery`), and OpenSearch snapshots through the
indexer's `_snapshot` API: `restore` prints those steps. `--dry-run` lists the
objects.

## `siem enroll <tenant>`

Reads the tenant's `enrolment-bundle` Secret (written by the reconciler with
dry-run off) and prints the Wazuh agent install script for `--os`
(`linux`, `macos`, `windows`) with how to run it and the Velociraptor client
hint. The script carries the tenant's enrolment password: treat the output as
a secret. `--output-dir` writes the script, `velociraptor-client.config.yaml`
and the hint there (mode 0600) instead.

## `siem quickstart`

A single machine, one demo tenant, no Argo CD and no kubeaid-config repository;
for evaluation in under 30 minutes. On a fresh Linux host (8 CPU / 24 GiB):

```sh
git clone https://github.com/Obmondo/KubeAid && cd KubeAid
./hack/kubesoc-quickstart.sh
```

The script installs k3s (unless a cluster is reachable), the prerequisites from
KubeAid's charts (sealed-secrets, cert-manager, CloudNativePG, mariadb-operator
and its CRDs, a self-signed ClusterIssuer), a development Keycloak (unless
`KEYCLOAK_URL` is set), and then runs:

```sh
kubeaid-cli siem quickstart --dir ./kubesoc-quickstart --kubeaid-dir . \
  --domain <node IP>.nip.io --agent-address <node IP> \
  --keycloak-url http://keycloak.<node IP>.nip.io/auth \
  --cluster-issuer kubesoc-selfsigned --run
```

which writes `k8s/kubesoc-quickstart/kubeaid-cli.general.yaml` (tenant `demo`,
agent ports 1515/1514, reconciler dry-run off), renders it, and writes and runs
`install.sh`: the sealed Secrets and `helm upgrade --install` of
`security-operations` and `wazuh-demo`, each with the `single` profile
overrides `hack/kubesoc-quickstart/values-single-<chart>.yaml`. Without `--run`
it only writes the files. Settings of the script: `NODE_IP`, `DOMAIN`,
`WORKDIR`, `KEYCLOAK_URL`, `CLUSTER_ISSUER`, `RECONCILER_TAG`, `AI_TRIAGE=1`,
`ASSUME_YES=1`. Self-signed certificates make browsers warn and can break SSO
between the components; use a real `DOMAIN` and
`CLUSTER_ISSUER=letsencrypt-prod` for a full SSO trial.

## `siem bundle` / `siem bundle import`

```sh
kubeaid-cli siem render --cluster-dir k8s/prod
kubeaid-cli siem bundle --cluster-dir k8s/prod --kubeaid-dir ../KubeAid \
  --content ./content --ollama-models ~/.ollama/models \
  --extra-image ghcr.io/cloudnative-pg/postgresql:16.4
# copy kubesoc-<version>.tar into the air-gapped network, then:
kubeaid-cli siem bundle import --bundle kubesoc-<version>.tar \
  --registry registry.example.com/kubesoc --extract-to ./kubesoc-bundle
```

`bundle` renders every SOC release in-process (`helm template` of the charts in
`--kubeaid-dir` with the values in `--cluster-dir`), collects the images of the
manifests (`image:` strings and `{registry, repository, tag}` maps, CNPG
`imageName`), pulls them for `--platform` (default `linux/amd64`, credentials
from `~/.docker/config.json`) into an OCI layout, and writes one tar with
`manifest.json` (version, chart revision, images with digests, the images of
each release), `images/`, `charts/` (symlinked sub-charts resolved), `content/`
and `models/`. Images the manifests do not name (an operator's default
PostgreSQL or MariaDB image, the prerequisites' images) go in with
`--extra-image`. `--skip-images` writes the manifest only. Without
go-containerregistry, `skopeo copy docker://<image> oci:images:<image>` per
`manifest.json` entry builds the same layout.

`import` pushes every image to `--registry`, keeping its repository path
(`wazuh/wazuh-manager:4.14.3` becomes
`registry.example.com/kubesoc/wazuh/wazuh-manager:4.14.3`; `--insecure` for a
plain HTTP registry). Point the nodes at it as a mirror (k3s
`/etc/rancher/k3s/registries.yaml`, containerd `hosts.toml`).

## The upgrade test

`pkg/siemctl/upgrade_test.go` renders the stack from the previous release's
cluster directory (`pkg/siemctl/testdata/upgrade/previous/`: its general config
and everything it rendered) with the current code, and fails when:

- any SealedSecret is re-encrypted or disappears (a credential rotated);
- any password hash, cluster key or MISP Valkey password in the rendered values
  changes;
- any Application of the previous render (a tenant's `wazuh-<code>`) is gone;
- the diff differs from the reviewed golden file `testdata/upgrade/current.diff`.

After a deliberate render change: `go test ./pkg/siemctl -run TestUpgradeRender
-update` and review `current.diff`. At a release, move the baseline forward
with `-update-baseline` (it re-renders `previous/` from
`testdata/upgrade/general.yaml`, keeping its credentials). It runs with
`go test ./pkg/...` in CI. `TestQuickstartRender` additionally renders the
real charts when `KUBESOC_KUBEAID_DIR` points at a KubeAid checkout.
