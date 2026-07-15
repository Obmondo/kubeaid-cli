# `kubeaid-cli backup status`

Date: 2026-07-15
Status: Approved → implementing

## Problem

Operators need a fast, at-a-glance answer to "are my backups healthy?" for a
KubeAid managed cluster, covering both CloudNativePG (logical dumps + WAL
archiving) and Velero. The in-cluster **backup-exporter** already computes this
and exposes it as Prometheus gauges; a companion exporter change adds the
convenience `GET /backups` JSON endpoint this command consumes. There is no
first-class CLI surface today — operators would otherwise port-forward the
Service and read raw gauges.

`kubeaid-cli backup status` reaches the exporter and renders a compact
backup-health table.

## Connectivity — current kube context, not bootstrap

This is a **normal kubectl-style client**. It resolves the cluster from the
operator's **current kube context** via the standard kubeconfig loading rules —
honoring `$KUBECONFIG`, `~/.kube/config`, `--kubeconfig` and `--context`. It has
**nothing** to do with cluster bootstrap: it does not read
`outputs/kubeconfigs/...` or any bootstrap output path.

It reaches the exporter through the **apiserver Service proxy** rather than
requiring a port-forward or an Ingress:

```
cs.CoreV1().Services(namespace).ProxyGet("http", service, port, "/backups", nil).DoRaw(ctx)
```

REST config is built from the default loading rules + overrides:

```go
loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
if kubeconfigFlag != "" { loadingRules.ExplicitPath = kubeconfigFlag }
overrides := &clientcmd.ConfigOverrides{}
if contextFlag != "" { overrides.CurrentContext = contextFlag }
cfg, _ := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
```

### RBAC requirement

Because the request goes through the Service proxy, the operator's user needs
**`get` on `services/proxy`** in the exporter's namespace (in addition to the
implicit `get`/`list` on `services`). Example minimal Role:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: monitoring
  name: backup-status-reader
rules:
  - apiGroups: [""]
    resources: ["services", "services/proxy"]
    verbs: ["get", "list"]
```

If this permission is missing, the fetch fails with a proxy `forbidden` error;
the command surfaces a hint pointing at `services/proxy`.

## Command surface

```
kubeaid-cli backup status [flags]
```

A `backup` group command (mirrors `config`) with a single `status` leaf, so
future backup subcommands (e.g. `backup trigger`) have a home.

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-n, --namespace` | `monitoring` | Namespace the exporter Service runs in (the exporter's default deployment namespace). Not required. |
| `--service` | `obmondo-backup-exporter` | Exporter Service name. |
| `--port` | `8080` | Exporter HTTP port serving `/backups`. |
| `--kubeconfig` | `""` | Kubeconfig path; empty ⇒ standard resolution / `$KUBECONFIG`. |
| `--context` | `""` | Kube context; empty ⇒ the kubeconfig's current-context. |
| `-o, --output` | `table` | `table` \| `wide` \| `json`. |

No default is tied to a bootstrap output.

> The `--service` default `obmondo-backup-exporter` assumes the exporter is
> installed under the canonical release name (the chart fullname). A
> differently-named Helm release yields a prefixed Service (e.g.
> `myrelease-obmondo-backup-exporter`); pass `--service` explicitly then.

## JSON contract (`GET /backups`)

HTTP 200, `application/json`:

```json
{
  "generated_at": "2026-07-15T11:26:03Z",
  "metrics": [
    {
      "name": "postgres_latest_logical_backup_age",
      "help": "seconds since the latest logical backup",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 3661 }
      ]
    }
  ]
}
```

Only the exporter's own families are present (names starting `postgres_`,
`cnpg_`, or `backup_exporter_`); Go runtime/process/promhttp families are
excluded. **Ages are in seconds.** A family with no current time series may be
omitted.

The 11 families and their label sets:

| Family | Labels |
| --- | --- |
| `postgres_logical_backup_max_interval` | namespace, cluster_name |
| `postgres_latest_logical_backup_age` | namespace, cluster_name |
| `postgres_oldest_logical_backup_age` | namespace, cluster_name |
| `cnpg_wal_backup_max_interval` | namespace, cluster_name |
| `postgres_latest_cnpg_wal_backup_age` | namespace, cluster_name |
| `postgres_oldest_cnpg_wal_backup_age` | namespace, cluster_name |
| `backup_exporter_postgres_error` | backup, namespace, cluster_name, type (0 ok, 1 error; backup = `logical`\|`wal`) |
| `backup_exporter_velero_latest_backup_age` | backup, namespace, resource_name, resource_type |
| `backup_exporter_velero_oldest_backup_age` | backup, namespace, resource_name, resource_type |
| `backup_exporter_velero_backup_max_interval` | namespace, resource_name, resource_type (no `backup` label) |
| `backup_exporter_velero_error` | type (0 ok, 1 error; global) |

## Grouping + rendering

Decode the payload and index samples by family name.

### PostgreSQL table — one row per (namespace, cluster_name)

Logical and WAL are columns of the same row:

- LOGICAL: last = `postgres_latest_logical_backup_age`, oldest =
  `postgres_oldest_logical_backup_age`, max gap =
  `postgres_logical_backup_max_interval`, status =
  `backup_exporter_postgres_error{backup="logical"}`.
- WAL: last = `postgres_latest_cnpg_wal_backup_age`, oldest =
  `postgres_oldest_cnpg_wal_backup_age`, max gap =
  `cnpg_wal_backup_max_interval`, status =
  `backup_exporter_postgres_error{backup="wal"}`.
- Overall STATUS: `DEGRADED` if either present method is in error; `OK` if at
  least one method is present and none is in error; `UNKNOWN` if neither method
  reported an error gauge. (A method whose error gauge is absent — e.g. a
  logical-only cluster with no WAL series — does not by itself drag the row to
  `DEGRADED`.)

Default columns: `NAMESPACE, CLUSTER, LOGICAL (glyph + humanized last age), WAL
(glyph + humanized last age), STATUS`. `-o wide` expands to grouped
`LOGICAL {LAST, OLDEST, MAX GAP}` and `WAL {LAST, OLDEST, MAX GAP}` + `STATUS`.

### Velero table — one row per (namespace, resource_name, resource_type, backup)

- LAST = `backup_exporter_velero_latest_backup_age`, OLDEST =
  `backup_exporter_velero_oldest_backup_age`, MAX GAP =
  `backup_exporter_velero_backup_max_interval` (keyed by namespace +
  resource_name + resource_type — **no** `backup` label).
- STATUS is derived from freshness: `DEGRADED` (overdue) when the latest age
  exceeds the max interval, `OK` when within interval (or no interval is set),
  `UNKNOWN` when there is no latest age. There is no per-row Velero error gauge.
- The **global** `backup_exporter_velero_error` drives a separate
  exporter-health note (a warning line listing the `type`s currently in error),
  not the row STATUS.

Columns: `NAMESPACE, RESOURCE, TYPE, METHOD, LAST, OLDEST, MAX GAP, STATUS`.
`METHOD` is the `backup` label value (e.g. `restic`, `kopia`, `snapshots`).

### Common

- Ages are humanized to a compact "N ago" using the most significant non-zero
  unit plus the next-smaller unit when it is non-zero (up to two adjacent
  units): `1h 1m ago`, `3d 4h ago`, `42s ago`. A missing series renders as `—`.
- Tables use the house lipgloss rounded-border style; OK cells are green,
  degraded red, unknown faint.
- Footer: `data as of <generated_at>`.
- `-o json` prints the raw `/backups` payload unmodified (a single trailing
  newline may be appended for terminal cleanliness).

Example (`table`, anonymized):

```
PostgreSQL backups
╭───────────┬──────────┬──────────────┬──────────────┬──────────╮
│ NAMESPACE │ CLUSTER  │ LOGICAL      │ WAL          │ STATUS   │
├───────────┼──────────┼──────────────┼──────────────┼──────────┤
│ demo      │ demo-db  │ ✓ 1h 1m ago  │ ✓ 1m 1s ago  │ ✓ OK     │
╰───────────┴──────────┴──────────────┴──────────────┴──────────╯

data as of 2026-07-15T11:26:03Z
```

## Code layout

- `cmd/kubeaid-core/root/backup/{backup.go,status.go}` — cobra group + leaf;
  registered via `RootCmd.AddCommand(backup.BackupCmd)` in `root.go`.
- `pkg/backup/` — testable logic:
  - `backup.go`: `Options`, `OutputFormat`, the `fetchBackups` package-level
    func-var seam (apiserver Service proxy), and `Status` orchestrator.
  - `parse.go`: JSON contract types + grouping + status derivation.
  - `render.go`: humanization + lipgloss table rendering.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` in helpers; the command
  boundary uses `assert.AssertErrNil`.

## Testing

`pkg/backup/backup_test.go` — testify, table-driven:

- `humanizeSeconds` boundary cases.
- `Parse` grouping + status derivation from a canned `/backups` fixture
  (healthy + degraded PostgreSQL, overdue Velero, omitted WAL family, global
  Velero error).
- `veleroStatus` freshness truth table.
- `Status` end to end with a stubbed `fetchBackups`: `-o json` verbatim
  passthrough, `table`/`wide` render smoke, fetch-error propagation, invalid
  `-o` rejection.

## Out of scope

- Watching / polling (`--watch`).
- Triggering backups (`backup trigger`) — the `backup` group leaves room.
- Exporter-side changes beyond the `GET /backups` endpoint this command
  consumes. **That endpoint is a companion exporter change and a hard
  prerequisite** — this command is inert until it merges to the exporter's
  `main` and rolls out.
