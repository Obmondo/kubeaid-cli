# Bare-metal provisioning — rescue-first OS + storage layout

> Status: design proposal. Not yet implemented — see "Implementation work".

Decide and approve the disk layout *before* the OS is installed, by doing
all provisioning work from the Hetzner rescue system. Identify nodes by
hostname (no on-disk state file). Never run a destructive disk operation on
a live node.

## Why

- Today the OS is installed *before* the disk layout is planned:
  installimage runs with defaults, the storage planner then plans *around*
  whatever it produced, and `installImage.vg0.rootVolumeSize` feeds only the
  planner — never the install.
- The re-run check (`isHBMSReachable`) is cluster-blind. "SSH answers"
  cannot tell a node provisioned for another cluster apart from a fresh
  one, so re-provisioning a node into a new cluster silently inherits stale
  state.

## Invariants

1. Destructive disk operations (`installimage`, `sfdisk`, `zpool create`)
   run **only from rescue** — never on a live, booted node.
2. installimage never runs without passing the single approval gate.
3. Booting into rescue is non-destructive, so it needs no consent. A data
   wipe always needs explicit operator approval. Two separate consents.
4. Recognition uses **exact-match** signals only — hostname, named ZFS
   objects, GPT partition labels — never partition-size comparison
   (MiB/MB/sector rounding makes size matching unreliable).
5. No persisted state file. Cluster identity lives in the hostname.

## Identity and state

- Hostname is `<cluster>-<group>-<index>`, set by the generated installimage
  config.
- `<group>` is `control-plane` or the node-group name. Detection keys on
  **cluster + group**; the index is cosmetic (uniqueness, plus a readable
  `kubectl get nodes`).
- Match the hostname against the *known* cluster and group names from
  general.yaml — never free-parse it. Both cluster and group names can
  contain `-`, so splitting on `-` is ambiguous.
- "Is storage done?" is a runtime check, not a flag: `zpool list primary`
  plus `zfs list` for the expected datasets.

## The flow

### Phase 1 — classify every declared host

```
GET /boot/{id}/rescue -> note `active`   (context only — never triggers a reset alone)

SSH reachable?  (TCP:22, then handshake with our key; short timeout + retries)
|
+- YES -> hostname?
|   +- rescue                          -> PROVISION    (already in rescue)
|   +- <this cluster>-<this group>-<n> -> OURS -> zpool list primary + zfs list:
|   |      +- pool + datasets present  -> DONE  -> skip
|   |      +- missing / partial        -> REDO-STORAGE
|   +- any other hostname              -> FOREIGN -> WIPE candidate
|
+- NO -> off / fresh / unreachable     -> PROVISION    (fresh)
         reach rescue: active -> reset;  inactive -> activate rescue -> reset
```

`GET /boot/{id}/rescue` reports whether rescue is *queued for the next
boot* — it is not "currently in rescue", and it auto-clears once the server
boots into rescue. It is context for the `SSH = NO` branch only; it never
decides a reset on its own and never authorises a wipe. There is no Robot
API endpoint for the currently-running OS, so "in rescue" can only be
detected in-band, via the hostname.

### Phases 2-5

```
Phase 2  Bring every non-DONE host into rescue
         already in rescue   -> nothing
         off + rescue queued -> reset
         off / foreign / redo, not queued -> activate rescue -> reset
         -> wait for SSH + hostname == rescue      (non-destructive — no consent)

Phase 3  From rescue: scan disks (lsblk, WWNs, NVMe/SATA) -> storage plan per host
         per node-group: AreStoragePlansAlike must hold

Phase 4  ONE approval gate — the per-server box:
            control-plane
              srv-1  fresh                      -> install
              srv-2  foreign (cluster acme-old) -> WIPE + reinstall
            node-group gpu-workers
              srv-3  provisioned (this cluster) -> skip
              srv-4  OS ok, storage incomplete  -> redo storage
         operator approves once; every wipe is shown up front

Phase 5  Execute (only past Phase 4), all from rescue:
         PROVISION    -> generate installimage config (layout + hostname)
                      -> installimage -> reboot -> carve storage (executor)
         REDO-STORAGE -> zap partitions 5/6 + any partial pool -> carve storage
                         (OS partitions 1-4 untouched — no installimage re-run)

-> all hosts ready -> hand off to CAPH / Cluster API
```

The storage executor stays a simple, non-idempotent script: it only ever
sees a clean storage area — fresh after installimage, or post-zap on the
redo path. Zapping partitions 5/6 on the redo path is safe because
incomplete storage means the node never finished provisioning, so there is
no data there.

## Failure handling

When the operator rejects a host's wipe at Phase 4:

- **control-plane host** — hard-fail the bootstrap. A control-plane node
  cannot be skipped: etcd quorum needs the full odd count.
- **node-group host** — skip it for this run. If the group would drop below
  its minimum count, hard-fail instead. The skip must propagate everywhere
  downstream: the storage plan, `AreStoragePlansAlike`, and the CAPH machine
  template.

## Implementation work

**0. Resolve the CAPH question first (blocker).** Trace how the storage
plan and `HetznerBareMetalMachineTemplate.Spec...InstallImage` reach CAPH
today. This decides whether kubeaid-cli runs installimage itself or hands
CAPH a generated config. The items below assume kubeaid-cli drives it —
confirm before committing to them.

1. **HRobot rescue plumbing** — `POST` / `GET` / `DELETE /boot/{id}/rescue`
   alongside the existing `/boot/{id}/linux` and `/reset/{id}` calls; a
   wait-for-rescue helper (SSH + `hostname == rescue`).
2. **Per-host classification** — the Phase 1 logic in `os_install.go`: SSH
   probe, then hostname, then rescue / ours / foreign, plus the `zpool` /
   `zfs` storage check for an "ours" node. Replaces the reachability-only
   skip.
3. **installimage config generation** — compose a custom installimage
   config (disk layout from the approved plan, plus `HOSTNAME`) and run
   installimage from rescue. Replaces the bare `/boot/{id}/linux` one-shot
   in `activateHRobotLinuxInstallation`.
4. **Scan-from-rescue** — move `generateStoragePlan`'s disk scan to the
   rescue system, and reorder `prerequisite_infrastructure.go` so the plan
   precedes the OS install.
5. **Approval gate** — extend the existing bordered approval box to be
   per-server-state-aware (install / wipe / skip / redo-storage).
6. **Storage executor** — keep it non-idempotent; ensure it runs only from
   rescue; add the redo-storage "zap partitions 5/6 + any partial pool
   first" step; stamp GPT partition labels (`kubeaid-os`, `kubeaid-zfs`,
   `kubeaid-ceph`) when carving.
7. **Failure handling** — control-plane wipe-rejection hard-fails;
   node-group skip with a minimum-count check and downstream propagation.
8. **Validation** — cluster and group names must be RFC-1123-label-safe,
   and `<cluster>-<group>-<index>` must stay <= 63 characters.

This is a sizable change to the bare-metal prerequisite phase, not a single
PR. Items 1-8 are roughly ordered, but several parallelise.

## Open threads

- **CAPH integration** (item 0) — the real prerequisite.
- **ZFS tooling in the target OS** — if the executor runs from rescue, the
  installed OS still needs `zfsutils-linux` to import the pool at boot, so
  the installimage config must install it into the target.
- **Index-assignment rule** — cosmetic, but one is needed (for example,
  enumeration order at install time).
- **Optional** — mount the on-disk partitions from rescue to enrich the
  approval box (for example, "disk holds cluster acme-old").
- Assumes no concurrent kubeaid-cli run against the same servers — a re-run
  resumes a dead run, not a live one.
