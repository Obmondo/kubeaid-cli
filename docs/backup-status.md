# `backup status`

Reports CNPG (PostgreSQL) and Velero (volume/PVC) backup health, as evaluated by the in-cluster
[backup-exporter](https://github.com/Obmondo/backup-exporter). Read-only: it changes nothing on
the cluster or in Git.

## Usage

```
kubeaid-cli backup status          # human-readable report
kubeaid-cli backup status -o json  # the exporter's response, verbatim
```

- Uses your current kubeconfig, like kubectl. If `kubectl get svc -n monitoring` works, so does
  this.
- Finds the exporter itself — `monitoring` first, then every namespace. Nothing to configure, no
  `general.yaml` involved.
- Reaches it by port-forward, exactly as `kubectl port-forward` would, so it works through
  netbird's `ClusterProxy`. Your identity needs what `kubectl port-forward` needs in the
  exporter's namespace.
- Always exits `0` when it can report, however unhealthy the backups are. Non-zero means it
  could not fetch.

## Output

```
collected 2h 57m ago: cnpg | velero

Operator errors:
  cnpg: s3_list_failed

NAMESPACE    RESOURCE       TYPE           STREAM    METHOD            LATEST AGE   STATUS
demo         demo-pgsql     cnpg_cluster   logical   CronJob           none         collector_error (cronjob_not_found)
demo         demo-pgsql     cnpg_cluster   wal       Barman            4h 9m        healthy
demo         demo-uploads   pvc            volume    PodVolumeBackup   14h 57m      healthy
monitoring   obs-postgres   cnpg_cluster   logical   cronjob           3d 2h        exceeds_rpo
monitoring   prom-data      pvc            volume    CSISnapshot       14h 57m      healthy
4 resources: 2 healthy, 1 exceeds_rpo, 1 collector_error
```

- **Freshness line** — how long ago the exporter last looked. One age when the collectors agree,
  per-collector when they don't (`collected: velero 5m ago | cnpg never`).
- **Operator errors** — failures with no single resource to blame, e.g. Velero cannot list its
  bucket. Everything below them is that much less trustworthy.
- **Rows** — one per backup stream, sorted by namespace then resource. A CNPG cluster gives a
  `logical` and a `wal` row.
- **`STREAM` / `METHOD`** — what is backed up, and what takes it. `METHOD` names the thing to go
  look at when a row fails: `CronJob` for CNPG's logical dump, `Barman` for its WAL archive (the
  barman-cloud plugin in the Cluster CR), and Velero's own `PodVolumeBackup` / `CSISnapshot`.
- **`LATEST AGE`** — age of the newest backup (`2h 57m`, `3d 2h`), `none` when there is none at
  all, `-` when the exporter published nothing to measure.
- **`STATUS`** — `healthy`, `exceeds_rpo`, `no_backup`, `collector_error` (reason in
  parentheses) or `unknown`.
- **Summary** — counts resources, not rows. A resource whose streams disagree is counted once,
  under its worst one, so the figures never add up to more than you have.
- Every row carries its namespace, so `backup status | grep -v healthy` gives you complete lines.

## When it cannot report

| Situation | What you see |
|---|---|
| backup-exporter not installed | "no backup-exporter service found ... is the backup-exporter chart installed?" |
| Its pod is not `Running` + `Ready` | "no ready pod behind service `<namespace>/<name>`" |
| Two exporters, neither in `monitoring` | Every `namespace/name` found, rather than a guess at which to use |
| The connection never comes up | A timeout after 15s, naming the pod |
