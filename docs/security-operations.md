# Security operations (multi-tenant SOC)

One block in `general.yaml` sets up a multi-tenant security operations stack on a cluster:

- the KubeAid `security-operations` chart (central side: central Wazuh search, DFIR-IRIS,
  MISP, Velociraptor, Ollama, optionally the [siem-reconciler](siem-reconciler.md)),
- one KubeAid `wazuh` release per tenant, in namespace `wazuh-<code>` (wazuh chart README,
  section 11),
- the Wazuh credentials of every release, generated into `secrets.yaml` and sealed.

The chart READMEs in KubeAid (`argocd-helm-charts/kubesoc/README.md`,
`argocd-helm-charts/kubesoc/wazuh/README.md`) describe what the charts do with these values.

## general.yaml

```yaml
cluster:
  securityOperations:
    enabled: true
    domain: example.com            # required
    hostPrefix: ""                 # hosts: <hostPrefix><component>.<domain>
    # chartRevision: my-branch     # KubeAid revision of the charts; default forks.kubeaid.version
    # configRevision: my-branch    # kubeaid-config revision of the values files; default HEAD
    keycloak:
      url: https://keycloak.example.com/auth  # required: the browser-facing issuer
      realm: soc                   # default soc
      # internalURL: http://keycloakx-http.keycloakx.svc/auth  # back channel; see "Reaching Keycloak"
      # hostAliasIP: 10.0.0.10     # deprecated, see "Reaching Keycloak"
    profile: standard              # single | standard (default) | ha; see "Deployment profiles"
    topologyKey: kubernetes.io/hostname  # failure domain the profile spreads replicas over
    agentHost: agents.example.com  # required: host name the agents dial
    agentAddress: 192.0.2.10       # required: external IP of the per-tenant agent Services
    agentPortBase: 20000           # default 20000
    ingressClassName: traefik      # default traefik
    clusterIssuer: letsencrypt-prod  # default: the ClusterIssuer kubeaid-cli renders
    reconciler:
      enabled: false               # default false
      imageRepository: ""          # default: the chart's (ghcr.io/obmondo/siem-reconciler)
      imageTag: ""                 # default: the chart's
      imagePullSecrets: []         # Secret names for a private registry, sealed by you
      dryRun: true                 # default true
    dashboardBreakGlass: false     # default false: Wazuh dashboards log in through Keycloak only
    content:                       # detection content as code (KubeAid kubesoc-content)
      enabled: false               # default false
      canary: "001"                # tenant rolled out first; default the first tenant
      extraLists: []               # more etc/lists/<name> to register in every tenant
    backup:                        # see "Backups"; everything off until a bucket exists
      enabled: false               # default false
      bucket: kubesoc-backups      # required with enabled
      endpoint: ""                 # default: AWS S3; e.g. https://s3.example.com
      region: us-east-1            # default us-east-1
      basePath: kubesoc            # default kubesoc
      credentialsSecret: kubesoc-backup-s3   # you seal it into every namespace concerned
      pathStyleAccess: true        # default true (MinIO, Ceph RGW)
      veleroNamespace: velero      # default velero
      snapshotRepositoryType: fs   # fs (default) | s3
      snapshotStorageClass: ""     # ReadWriteMany; default: sharedStorageClass
      snapshotVolumeSize: 50Gi     # default 50Gi, per Wazuh release
      retentionDays: 30            # default 30
      verifyRestore: false         # default false: it creates and deletes a CNPG Cluster
    tenants:
      - code: "001"                # ^[a-z0-9]{1,32}$, unique
        name: Tenant A             # unique; IRIS customer, Velociraptor org
      - code: "002"
        name: Tenant B
        retentionDays: 90          # optional, chart default otherwise
        indexerReplicas: 1         # default 1
        expectedGBPerDay: 2        # default 0.5; sizes the indexer volume
        indexerStorageSize: ""     # optional explicit override, e.g. 300Gi
      - code: globex
        name: Tenant C
        agentPorts: {registration: 21005, events: 21004}  # required for a non-numeric code
```

Host names: central `wazuh`, `iris`, `misp` and `velociraptor` are
`<hostPrefix><component>.<domain>`; a tenant's dashboard is `<hostPrefix>wazuh-<code>.<domain>`.

Indexer volume: each tenant's indexer volume is sized
`retentionDays x expectedGBPerDay x 1.5` (headroom for merges and translog),
rounded up to whole Gi and never below 10Gi; `retentionDays` falls back to the
chart's 365 and `expectedGBPerDay` to 0.5. `indexerStorageSize` overrides it.
Every replica holds a full copy, so the derived size is per volume and
`indexerReplicas` multiplies the cluster total. Retention itself is enforced by
the reconciler's ISM policies (`docs/siem-reconciler.md`); this only sizes the
disk it needs.

A rendered size reaches a running tenant only through a new PVC: a StatefulSet's
volume template cannot be patched, and a PVC never shrinks. Lowering the size (or
the retention) is therefore safe but has no effect on existing volumes; raising it
needs, per tenant, `kubectl -n wazuh-<code> patch pvc wazuh-indexer-wazuh-indexer-0
-p '{"spec":{"resources":{"requests":{"storage":"<size>"}}}}'` on every replica
(the StorageClass must allow expansion), then
`kubectl -n wazuh-<code> delete statefulset wazuh-indexer --cascade=orphan` and a
sync of the tenant Application, which re-creates the StatefulSet around the
running pods.

Agent ports: for an all-digit code, registration is `agentPortBase + code*10 + 5` and events
`agentPortBase + code*10 + 4` (tenant `001`: 20015 and 20014). Every tenant gets a Service
`wazuh-agents` with `externalIPs: [agentAddress]` on its own port pair, forwarding to 1515 and
1514 on its manager; agents dial `agentHost` on those ports. All ports must be unique and at
most 65535.

### Detection content

With `content.enabled`, the rules, decoders, CDB lists and agent group configs of the KubeAid
`kubesoc-content` chart (its README is the authoring guide) are what the tenants run, and the
`siem-reconciler` uploads them to every manager over the API: canary tenant first, validated,
hot-reloaded, rolled back on failure. kubeaid-cli then:

- stops rendering `wazuh.localRules` and the hand-copied `<ruleset>` block in each tenant's
  `master.extraConf`, and instead registers the MISP lists (plus `content.extraLists`) and
  excludes the stock IoC file through `wazuh.ruleset.extraLists` / `extraRuleExcludes`;
- turns the Velociraptor `customArtifacts` mount off, since the reconciler sets the artifacts
  with `artifact_set` (no server restart);
- adds `kubesoc-content.enabled` (and `canary`) to the central values.

Off, everything renders exactly as before. Registering a new list restarts the managers (it is
an `ossec.conf` change); the rules themselves never need a restart.

## secrets.yaml

Used by the cluster commands (bootstrap, upgrade); `kubeaid-cli siem render` does not use it
(next section). Filled in on every run when blank (like the NetBird and Keycloak secrets);
existing passwords are never changed:

```yaml
securityOperations:
  central:
    indexerPassword: <32 random alphanumerics>
    indexerPasswordHash: $2a$12$...   # bcrypt, cost 12
    dashboardPassword: ...
    dashboardPasswordHash: ...
  tenants:
    "001":
      indexerPassword: ...
      indexerPasswordHash: ...
      dashboardPassword: ...
      dashboardPasswordHash: ...
      apiPassword: Aa1.<28 random alphanumerics>   # meets the Wazuh API password policy
      authdPassword: ...
      clusterKey: ...                               # manager cluster key, sealed as wazuh-manager-cluster-key
```

A hash is regenerated only when it is empty or no longer matches its password, so renders
are stable across runs and follow a password you change by hand. Entries of tenants removed
from `general.yaml` are left in place.

## Rendered files

Relative to the cluster directory (`k8s/<cluster>` in the kubeaid-config repository), for two
tenants:

```
argocd-apps/templates/security-operations.yaml   # Apps security-operations (sync wave 60),
                                                 # wazuh-001 and wazuh-002 (61)
argocd-apps/values-security-operations.yaml      # central values
argocd-apps/values-wazuh-tenant.yaml             # values shared by every tenant release
sealed-secrets/security-operations/wazuh-indexer-cred.yaml
sealed-secrets/security-operations/wazuh-dashboard-cred.yaml
sealed-secrets/wazuh-001/wazuh-indexer-cred.yaml
sealed-secrets/wazuh-001/wazuh-dashboard-cred.yaml
sealed-secrets/wazuh-001/wazuh-api-cred.yaml
sealed-secrets/wazuh-001/wazuh-authd-pass.yaml
sealed-secrets/wazuh-001/wazuh-manager-cluster-key.yaml   # sealed once, see below
sealed-secrets/wazuh-002/...                     # the same five
sealed-secrets/security-operations/misp-redis.yaml        # sealed once, see below
security-operations/sealed-secrets/security-operations/wazuh-indexer-cred.yaml
security-operations/sealed-secrets/security-operations/wazuh-dashboard-cred.yaml
security-operations/sealed-secrets/wazuh-001/wazuh-indexer-cred.yaml
security-operations/sealed-secrets/wazuh-001/wazuh-dashboard-cred.yaml
security-operations/sealed-secrets/wazuh-001/wazuh-api-cred.yaml
security-operations/sealed-secrets/wazuh-001/wazuh-authd-pass.yaml
security-operations/sealed-secrets/wazuh-002/...  # the same four
```

Each `wazuh-<code>` Application reads `values-wazuh-tenant.yaml` and carries the tenant's own
part inline (`helm.valuesObject`): certificate organization `tenant-<code>`, IRIS customer
(tenant name), indexer replicas, password hashes, dashboard host, agent Service and the
dashboard role mappings (`administrator` -> all_access, `analyst` and `tenant-<code>` ->
readall + kibana_user). Adding a tenant adds one Application and one entry in the central
values; nothing of the existing tenants changes.

No secret value is rendered in plaintext: the Applications and values files hold only
bcrypt hashes and Secret names. The tenant releases read the manager cluster key from
`wazuh-manager-cluster-key` (wazuh chart `wazuh.clusterKeySecret`), and MISP and its Valkey
read their password from `misp-redis` (misp chart `env.redisPasswordSecret`, valkey
`usersExistingSecret`). Both are sealed only while their sealed file is missing and are
never rotated by a render; delete the file to get a new value (then restart the managers,
or MISP, misp-modules and Valkey).

The Wazuh dashboards (central and tenants) offer only the Keycloak login;
`dashboardBreakGlass: true` adds the username/password form for the internal `admin`
(password in `wazuh-indexer-cred`). The indexers keep only `admin` and `kibanaserver` as
internal users (wazuh chart README, section 8a).

`cluster bootstrap` renders these files with the rest of the cluster's files and syncs
`security-operations` as an ordered step (after keycloakx and netbird), so the tenant
namespaces exist before the `wazuh-<code>` Apps sync.

## kubeaid-cli siem render

For a cluster whose other kubeaid-config files are maintained by hand:

```sh
kubeaid-cli siem render \
  --cluster-dir ~/src/kubeaid-config/k8s/<cluster> \
  --sealed-secrets-cert ./sealed-secrets.pem
```

It reads only `forkURLs` and `cluster.securityOperations` from the general config, by default
`kubeaid-cli.general.yaml` in `--cluster-dir` (add the block there), then writes only the files
listed above and prints them. It needs no `secrets.yaml`, no cloud credentials and runs no git
operation: review the diff, commit and push yourself.

The Wazuh passwords exist only inside the sealed Secrets; the rendered values hold their
bcrypt hashes. A release (the central search `security-operations`, or one tenant
`wazuh-<code>`) whose sealed files and hashes are already in `--cluster-dir` is left exactly
as it is. A new tenant (nothing rendered for it yet) gets fresh passwords.

Nothing is rotated silently: when a release exists (its Application, the central values or any
of its sealed files is there) but a sealed file, a password hash or the cluster key is missing,
the render stops and lists what is missing, and writes nothing. Restore the files from git, or
rotate on purpose:

```sh
kubeaid-cli siem render --cluster-dir ... --rotate=wazuh-001      # one tenant
kubeaid-cli siem render --cluster-dir ... --rotate=security-operations,wazuh-002
kubeaid-cli siem render --cluster-dir ... --rotate                # every release
```

(Use `--rotate=<release>`, with `=`: a bare `--rotate` means every release.) Rotated passwords
reach the running Wazuh once the new SealedSecrets are synced and its pods restart. The
plaintext of a running release can be read back from the cluster (`kubectl get secret`) if it
is ever needed.

- `--cluster-dir` (required): a local checkout of `k8s/<cluster>`.
- `--general-config`: another file holding `forkURLs` and `cluster.securityOperations`.
- `--sealed-secrets-cert`: the sealed-secrets controller's public certificate (file or URL).
  Without it the certificate is fetched from the controller (`sealed-secrets` namespace)
  through the cluster `$KUBECONFIG` points at, which needs read access to the service proxy.
  A sealed file keeps its ciphertext while its plaintext and the certificate are unchanged.
- `--rotate[=<release>,...]`: generate new credentials for existing releases (see above).
- `--remove-legacy-sealed-secrets`: step 2 of the migration below.

### Moving a cluster to sealed cluster keys and Valkey password

A cluster directory rendered before these two Secrets existed has the cluster keys in the
`wazuh-<code>` Applications (`wazuh.wazuh.key`) and the Valkey password in the central
values (`misp.misp.env.redisPassword`). The next `siem render` seals exactly those values
into the new Secrets and drops them from the rendered files; no other sealed file changes
and nothing is rotated. The charts must be a KubeAid revision that knows
`wazuh.clusterKeySecret` and the misp `env.redisPasswordSecret` (`chartRevision`), or MISP
cannot reach Valkey. The values stay in the git history: rotate them if that matters (delete
the sealed files, render, restart as above). Order on the cluster: sync `secrets` first (the
Secrets must exist), then `security-operations` and the `wazuh-<code>` Applications. The
securityadmin hook Job removes the OpenSearch demo users on that sync.

### How the sealed Secrets reach the cluster

Each SOC Application owns the sealed Secrets of its namespace: besides the chart and the
`$values` ref it has a third source, `k8s/<cluster>/security-operations/sealed-secrets/<namespace>`
(directory, recursive). The shared `secrets` Application (`k8s/<cluster>/sealed-secrets/`, prune
and self-heal) never sees them, so a bad change there cannot prune the SOC's credentials. Any
other SealedSecret of a SOC namespace dropped into that directory (hand-sealed OIDC or API key
Secrets, for example) is synced by the same Application.

Every SealedSecret kubeaid-cli renders there carries two annotations on the SealedSecret object
(not on the Secret it creates):

- `argocd.argoproj.io/sync-options: Prune=false`: no Application ever deletes it, whatever its
  prune setting, including after a tenant is removed from `general.yaml`.
- `argocd.argoproj.io/sync-wave: "-1"`: applied before the workloads that read it.

Since the `wazuh-<code>` Applications create their namespace (`CreateNamespace=true`), their
SealedSecrets no longer fail when they sync before `security-operations`; the sync order is
still `root`, `security-operations`, then the `wazuh-<code>` Applications.

### Migrating a cluster rendered before (Secrets under sealed-secrets/)

Older versions rendered these files to `k8s/<cluster>/sealed-secrets/<namespace>/`. Moving
them must never leave a moment in which no Application wants the objects while the `secrets`
app prunes. `siem render` does it in two commits:

1. Run `siem render` (no other flag). For each SOC Secret still in `sealed-secrets/` and not yet
   in its owned directory it copies the file byte for byte (same ciphertext, no re-sealing, no
   rotation) to `security-operations/sealed-secrets/<namespace>/`, adds the two annotations to
   both copies, and renders the Applications with the new source. It lists the legacy files it
   left in place. Commit and push, then:
   1. let `secrets` sync (it is automated): the live SealedSecrets now carry `Prune=false`;
   2. sync `root` (the Applications gain their third source);
   3. sync `security-operations`, then each `wazuh-<code>`: they adopt the existing
      SealedSecrets (same content, nothing is deleted or re-created, the Secrets and the
      running pods are untouched). Until step 2 both apps apply the same objects; Argo CD shows
      a shared-resource warning and the tracking label may flip between them. That is harmless,
      because both copies are identical.
2. Run `siem render --remove-legacy-sealed-secrets`. It deletes the legacy copies (only those
   whose owned copy exists; hand-sealed files and their directories stay). Commit and push,
   then sync the SOC Applications once more. The `secrets` app no longer wants the objects and,
   because of `Prune=false`, never deletes them; after the SOC sync they belong to the SOC
   Applications only.

Rotating (`--rotate`) during the migration rewrites both copies, so the two apps never apply
different ciphertexts. Hand-sealed SOC Secrets can be moved the same way: add
`argocd.argoproj.io/sync-options: Prune=false` to the SealedSecret in `sealed-secrets/`, let
`secrets` sync, copy it into the owned directory, sync the SOC App, then delete the old file.

### Sync policy

`cluster.securityOperations.sync` sets the Applications' `syncPolicy`:

```yaml
sync:
  automated: false        # true: automated sync with selfHeal
  prune: false            # needs automated; lets automated sync delete what left git
  serverSideApply: null   # default: the value of automated; adds ServerSideApply=true
```

With the defaults the Applications keep syncing by hand, as before. Whatever `prune` says,
the SealedSecrets above are never pruned, and neither are the tenant namespaces (the
`security-operations` chart marks them `Prune=false,Delete=false`; namespaces created through
`CreateNamespace=true` are never deleted by Argo CD). The Applications carry
`argocd.argoproj.io/sync-wave` 60 (`security-operations`) and 61 (`wazuh-<code>`) under the root
Application. Argo CD waits for the health of a wave's Applications only when the Application
health check is enabled in `argocd-cm` (`resource.customizations.health.argoproj.io_Application`);
without it the waves only order the applies.

## Deployment profiles

`securityOperations.profile` sizes the stack. It changes only what the rendered values files
say; the charts do the rest.

| | `single` | `standard` (default) | `ha` |
|---|---|---|---|
| Tenant indexer nodes | 1 | 1 | 3, spread over `topologyKey`, `minAvailable: 2` |
| Shard copies of the alert indices | 0 | 0 | 1 (with allocation awareness) |
| Wazuh manager | master only | master only | master + 2 workers behind the agent Service |
| Wazuh master budget | none | none | `minAvailable: 1` |
| Central dashboard | 1 | 1 | 2, budget, spread |
| IRIS app / worker | 1 | 1 | 2 each, budgets, spread |
| PostgreSQL, RabbitMQ, MariaDB | 1 | 1 | 3 each |
| Velociraptor | 1 | 1 | 1 with a budget (its datastore has one writer) |

`standard` renders exactly what every release rendered before profiles existed, and `single`
renders the same thing — they differ only in that `single` never adds a budget, so a
single-node cluster can always be drained. Everything new is in `ha`.

Two things `ha` needs that it cannot arrange for itself:

- **`sharedStorageClass` (ReadWriteMany)**, or the second IRIS app pod cannot mount the
  volume and the update strategy stays `Recreate`.
- **As many failure domains as replicas.** With `topologyKey:
  topology.kubernetes.io/zone` and three indexer nodes, a cluster with two zones leaves the
  third pod Pending — the indexer spread is `DoNotSchedule` on purpose, because a third
  copy on a node that already holds one is not a third copy.

A per-tenant `indexerReplicas` always wins over the profile's.

## Backups

`securityOperations.backup` is the one place backups are turned on. It fans out to four
backends, because no single one covers everything (security-operations README, "Backups"):

| What | Backend | Where it lands |
|---|---|---|
| Volumes: Wazuh manager data (`client.keys`) and indexer data, the Velociraptor datastore, the IRIS files, the MISP attachments | Velero `Schedule` (Kopia), 14 daily and 4 weekly | `backup.velero` in the chart |
| Alert indices | OpenSearch snapshots, one `CronJob` per indexer | `backup.opensearch` |
| The IRIS database | CloudNativePG barman object store with WAL archiving | `dfir-iris.global.postgresql.backups` |
| The MISP database | mariadb-operator `Backup` with a schedule | `misp.externalMariadb.backup` |

Steps for an operator:

1. Create the bucket and an access key for it.
2. Seal a Secret named `credentialsSecret` with the keys `ACCESS_KEY_ID` and
   `ACCESS_SECRET_KEY` into **the Velero namespace, `security-operations` and every
   `wazuh-<code>` namespace**. kubeaid-cli does not seal it: it is the object store's
   credential, not the SOC's.
3. Fill in `bucket`, `endpoint`, `region` and, with the default `snapshotRepositoryType: fs`,
   a ReadWriteMany `snapshotStorageClass` (or `sharedStorageClass`).
4. Set `enabled: true` and render.

`snapshotRepositoryType: s3` needs an indexer image with the **`repository-s3` plugin**,
which the stock `wazuh/wazuh-indexer` image does not ship, and the bucket keys in the
indexer's keystore. The `fs` default needs only a shared volume.

`verifyRestore: true` adds a monthly Job that recovers the newest IRIS database backup into a
scratch CloudNativePG cluster, queries it and deletes it again. It creates and deletes a
`Cluster`, so it is off by default.

`kubeaid-cli siem backup` and `siem restore` drive what is rendered here: they select by the
label `kubesoc.io/backup-set` and find the Velero `Schedule`, the CloudNativePG
`ScheduledBackup`, the MariaDB `Backup` and the snapshot `CronJob`s.

## Reaching Keycloak

`keycloak.url` is the browser-facing URL and the issuer in every token. The pods have to
reach Keycloak too, and a Keycloak published only by an internal ingress does not resolve to
anything useful from inside the cluster.

- **`keycloak.internalURL`** is the same Keycloak as the pods reach it, normally a Service
  DNS name. Every back-channel call uses it: the reconciler's admin API, the Velociraptor
  Keycloak sync, and the Wazuh dashboard's and indexer's OIDC discovery. The issuer stays
  public and still matches, because Keycloak's own frontend URL decides what goes into its
  discovery document.
- **A CoreDNS rewrite** covers Velociraptor, IRIS and MISP, which fetch discovery from the
  issuer itself and refuse a mismatch. In KubeAid's `coredns` chart:

  ```yaml
  rewrites:
    - from: keycloak.example.com
      to: traefik-internal.traefik.svc.cluster.local
  ```

- **`keycloak.hostAliasIP` is deprecated.** It pins the host name to an ingress Service's
  ClusterIP in every SOC pod; that address changes when the Service is recreated, and SSO
  then breaks everywhere at once until someone renders again. It still works, as a stop-gap.

## What stays manual

- The OIDC client Secrets (`wazuh-dashboard-oidc` in every Wazuh namespace, `dfir-iris-oidc`,
  `velociraptor-oidc`, `oidc-credentials`) are created by the siem-reconciler together with the
  Keycloak clients. With the reconciler off, create clients and Secrets by hand; the Wazuh
  indexer and dashboard pods wait for `wazuh-dashboard-oidc`. MISP's `oidc-credentials` still
  needs its `username` key set by hand (security-operations chart README, section 6).
- Keycloak reachability: the Wazuh indexers and dashboards may reach TCP 443/8443 in the
  `traefik` namespace (Keycloak behind the ingress controller in the same cluster). A Keycloak
  elsewhere needs other egress rules; Velociraptor uses its chart's `oidcEgressCIDRs`.
- The MISP to Wazuh CDB export stays off (`misp.wazuhCdbExport.enabled`); its targets are
  rendered.
- `agentAddress` must be routed to a node (for example a failover or floating IP), and
  `agentHost` must resolve to it.
