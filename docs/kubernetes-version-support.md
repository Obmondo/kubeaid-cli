# Kubernetes version support

Every Kubernetes version you request is validated at bootstrap. It must:

- **start with `v`** — for example `v1.34.0`;
- be a **released** version that is **not past end-of-life** — end-of-life is checked against
  [endoflife.date](https://endoflife.date/kubernetes) data baked into the binary (refresh it with
  `make fetch-k8s-eol`);
- be **within the range supported for your provider**:

| KubeAid CLI | AWS · Azure · Hetzner (Cluster API) | Bare metal (KubeOne) |
|---|---|---|
| `v0.31.x` | `v1.30` → latest released (non-EOL) | `v1.33` – `v1.35` |

- **Cluster API clouds** — `v1.30` up to the latest released minor.
- **Bare metal** — fixed to `v1.33`–`v1.35` by **KubeOne v1.13**; the range moves when KubeOne is upgraded.
- **KubePrometheus** — matched to the Kubernetes version automatically, over `v1.32`–`v1.36` (`cgroup v1`
  support ends at `v1.35`).

> **Maintainers:** update the table each release when the supported range, KubeOne version, or a pinned
> component changes.
