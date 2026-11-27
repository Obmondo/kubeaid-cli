# Security operations (multi-tenant SOC)

One block in `general.yaml` sets up a multi-tenant security operations stack on a cluster:

- the KubeAid `security-operations` chart (central side: central Wazuh search, DFIR-IRIS,
  MISP, Velociraptor, Ollama, optionally the [siem-reconciler](siem-reconciler.md)),
- one KubeAid `wazuh` release per tenant, in namespace `wazuh-<code>` (wazuh chart README,
  section 11),
- the Wazuh credentials of every release, generated into `secrets.yaml` and sealed.

The chart READMEs in KubeAid (`argocd-helm-charts/security-operations/README.md`,
`argocd-helm-charts/wazuh/README.md`) describe what the charts do with these values.

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
      url: https://keycloak.example.com/auth  # default https://<cluster.keycloak.dns>/auth
      realm: soc                   # default soc
      # hostAliasIP: 10.0.0.10     # pin the Keycloak host in every pod (internal-only Keycloak)
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
    tenants:
      - code: "001"                # ^[a-z0-9]{1,32}$, unique
        name: Tenant A             # unique; IRIS customer, Velociraptor org
      - code: "002"
        name: Tenant B
        retentionDays: 90          # optional, chart default otherwise
        indexerReplicas: 1         # default 1
      - code: globex
        name: Tenant C
        agentPorts: {registration: 21005, events: 21004}  # required for a non-numeric code
```

Host names: central `wazuh`, `iris`, `misp` and `velociraptor` are
`<hostPrefix><component>.<domain>`; a tenant's dashboard is `<hostPrefix>wazuh-<code>.<domain>`.

Agent ports: for an all-digit code, registration is `agentPortBase + code*10 + 5` and events
`agentPortBase + code*10 + 4` (tenant `001`: 20015 and 20014). Every tenant gets a Service
`wazuh-agents` with `externalIPs: [agentAddress]` on its own port pair, forwarding to 1515 and
1514 on its manager; agents dial `agentHost` on those ports. All ports must be unique and at
most 65535.

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
      clusterKey: ...                               # wazuh.key of the tenant's manager
```

A hash is regenerated only when it is empty or no longer matches its password, so renders
are stable across runs and follow a password you change by hand. Entries of tenants removed
from `general.yaml` are left in place.

## Rendered files

Relative to the cluster directory (`k8s/<cluster>` in the kubeaid-config repository), for two
tenants:

```
argocd-apps/templates/security-operations.yaml   # Apps security-operations (sync-order 60),
                                                 # wazuh-001 and wazuh-002 (61)
argocd-apps/values-security-operations.yaml      # central values
argocd-apps/values-wazuh-tenant.yaml             # values shared by every tenant release
sealed-secrets/security-operations/wazuh-indexer-cred.yaml
sealed-secrets/security-operations/wazuh-dashboard-cred.yaml
sealed-secrets/wazuh-001/wazuh-indexer-cred.yaml
sealed-secrets/wazuh-001/wazuh-dashboard-cred.yaml
sealed-secrets/wazuh-001/wazuh-api-cred.yaml
sealed-secrets/wazuh-001/wazuh-authd-pass.yaml
sealed-secrets/wazuh-002/...                     # the same four
```

Each `wazuh-<code>` Application reads `values-wazuh-tenant.yaml` and carries the tenant's own
part inline (`helm.valuesObject`): certificate organization `tenant-<code>`, IRIS customer
(tenant name), indexer replicas, password hashes, dashboard host, agent Service and the
dashboard role mappings (`administrator` -> all_access, `analyst` and `tenant-<code>` ->
readall + kibana_user). Adding a tenant adds one Application and one entry in the central
values; nothing of the existing tenants changes.

`cluster bootstrap` renders these files with the rest of the cluster's files and syncs
`security-operations` as an ordered step (after keycloakx and netbird), so the tenant
namespaces exist before the `secrets` and `wazuh-<code>` Apps sync.

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
bcrypt hashes. A release (the central search, or one tenant) whose sealed files and hashes
are already in `--cluster-dir` is left exactly as it is. A new tenant, or one whose sealed
files are missing, gets fresh passwords; delete a tenant's sealed files to rotate them (its
pods then need a restart). The plaintext of a running release can be read back from the
cluster (`kubectl get secret`) if it is ever needed.

- `--cluster-dir` (required): a local checkout of `k8s/<cluster>`.
- `--general-config`: another file holding `forkURLs` and `cluster.securityOperations`.
- `--sealed-secrets-cert`: the sealed-secrets controller's public certificate (file or URL).
  Without it the certificate is fetched from the controller (`sealed-secrets` namespace)
  through the cluster `$KUBECONFIG` points at, which needs read access to the service proxy.
  A sealed file keeps its ciphertext while its plaintext and the certificate are unchanged.

### How the sealed Secrets reach the cluster

The `secrets` Application syncs `k8s/<cluster>/sealed-secrets/` recursively with prune and
self-heal; each SealedSecret carries its own namespace. The tenant namespaces `wazuh-<code>`
are created by the `security-operations` chart (not by `secrets`), so on an existing cluster
sync in this order after pushing: `root` (creates the new Applications), `security-operations`,
`secrets` (a first `secrets` sync before the namespaces exist fails for the new
SealedSecrets; sync it again), then the `wazuh-<code>` Applications.

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
