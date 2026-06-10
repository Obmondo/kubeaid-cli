- - -
## v0.24.0 - 2026-06-10
#### Features
- (**bootstrap**) surface InfrastructureProvider conditions during sync - (489c28e) - Ashish Jaiswal
- (**config**) move the interactive prompt to `config generate` - (3e4c039) - Ashish Jaiswal
- (**hetzner**) drop HCloud API token requirement for pure bare-metal - (1de68d8) - Ashish Jaiswal
- (**hetzner**) auto-create vSwitch for pure bare-metal mode - (791087f) - Ashish Jaiswal
- (**netbird**) pause bootstrap until operator API-key Secret exists - (4316883) - Ashish Jaiswal
- (**prompt**) implement interactive config resume and state management - (27f9aaf) - Shubham Gupta
- (**prompt**) hetzner bare-metal UX polish + hybrid mode + secrets quoting - (25b4c50) - Ashish Jaiswal
- (**prompt**) hetzner bare-metal hosts via add-loop with Robot validation - (929f931) - Ashish Jaiswal
- (**storageplan**) render the approval layout as a bordered box - (fb3ed7d) - Ashish Jaiswal
- (**storageplan**) compact per-group composition + ZFS sub-volume summary - (b2195ea) - Ashish Jaiswal
- (**verify**) check Keycloak + NetBird endpoints before declaring bootstrap done - (082c073) - Ashish Jaiswal
- add Obmondo support configuration and validation in prompts and tests - (2a8516c) - Shubham Gupta
- enhance prompt configuration and validation logic also add comprehensive tests - (93cff46) - Shubham Gupta
#### Bug Fixes
- (**argo**) upsert the kubeaid-agent project role so re-runs are idempotent - (9849bda) - Ashish Jaiswal
- (**argo**) wait only for child Apps the root sync actually created - (fa227e4) - Ashish Jaiswal
- (**argocd-sync**) classify all codes.Unavailable as transient - (02acad0) - Ashish Jaiswal
- (**argocd-sync**) retry Sync on transient port-forward failures - (15d23ef) - Ashish Jaiswal
- (**capi**) drop customer-id suffix from InfrastructureProvider name - (8759a43) - Ashish Jaiswal
- (**capi-cluster**) pin kubeaid-storagectl version to kubeaid-cli release - (709e2ab) - Ashish Jaiswal
- (**ccm**) run hcloud CCM as a DaemonSet to dodge hostPort surge deadlock - (efbf6ce) - Ashish Jaiswal
- (**config**) require hetzner.apiToken for every mode including bare-metal - (d7f7cd4) - Ashish Jaiswal
- (**git**) prompt to retry when GPG commit signing fails mid-bootstrap - (67247c8) - Ashish Jaiswal
- (**hetzner**) render controlPlane.endpoint.host for bare-metal clusters - (df7e8c4) - Ashish Jaiswal
- (**hetzner**) always emit hcloud key in cloud-credentials, including bare-metal - (5e2a361) - Ashish Jaiswal
- (**hetzner**) skip kubeaidStoragectl block for non-bare-metal clusters - (dcca23a) - Ashish Jaiswal
- (**hetzner**) emit controlPlane.bareMetal.zfs.size to satisfy CAPH chart - (29fc46d) - Ashish Jaiswal
- (**hetzner**) populate controlPlane.regions from Robot DCs on bare-metal - (119c94c) - Ashish Jaiswal
- (**hetzner**) route storage-plan SSH through SSH_AUTH_SOCK - (ea15615) - Ashish Jaiswal
- (**hetzner**) route HBMS reachability probe through SSH_AUTH_SOCK - (03846af) - Ashish Jaiswal
- (**hetzner**) reuse SSH key registered under a different name - (87c483e) - Ashish Jaiswal
- (**installimage**) default bare-metal image to Ubuntu 26.04 zstd - (632e3d5) - Ashish Jaiswal
- (**k3d**) scope bootstrap cluster name to target cluster - (7a02a11) - Ashish Jaiswal
- (**netbird**) override dashboard AUTH_SUPPORTED_SCOPES to match Keycloak scope - (1397500) - Ashish Jaiswal
- (**netbird**) verify the bootstrap operator is on the right mesh - (96c9609) - Ashish Jaiswal
- (**secrets**) drop trim markers on comment blocks in cloud-credentials templates - (a22c741) - Ashish Jaiswal
- (**yubikey-prompt**) skip "Tap YubiKey" hint on file-backed SSH paths - (31ddff5) - Ashish Jaiswal
#### Documentation
- (**bare-metal**) document CheckDisk permanent-error recovery - (7d5ca5f) - Ashish Jaiswal
- (**post-bootstrap**) rewrite break-glass section with concrete commands - (78b180b) - Ashish Jaiswal
- (**todo**) capture findings from the netbird-mgmt-com bootstrap - (654af90) - Ashish Jaiswal
- (**todo**) record storagectl dev-version detect + helm-values preflight - (48b968f) - Ashish Jaiswal
- (**todo**) record the kubeaid-cli login mesh-check follow-up - (135e61b) - Ashish Jaiswal
- add Puppet Server certificate generation guide via HTTP API - (a71deb9) - Shubham Gupta
- rewrite bare-metal-provisioning to match the actual flow - (8fc2435) - Ashish Jaiswal
- add rescue-first bare-metal provisioning design - (8ecfc8f) - Ashish Jaiswal
#### Refactoring
- (**capi**) drop per-customer namespace suffix from GetCapiClusterNamespace - (c4773bd) - Ashish Jaiswal
- (**hetzner**) drop static zfs.size literals, persist on approval instead - (f2b26e1) - Ashish Jaiswal
- change module name to github.com/Obmondo/kubeaid-cli/ - (8c1d671) - Shubham Gupta
#### Miscellaneous Chores
- (**bootstrap**) show per-substep progress through the bare-metal path - (e20a1b7) - Ashish Jaiswal
- (**bootstrap**) hide OIDC issuer re-probe from the progress bar - (f2a59b1) - Ashish Jaiswal
- (**config**) trim verbose struct docs leaking into sample configs - (cb1d676) - Ashish Jaiswal
- (**hetzner**) pool SSH connections per host within prereq-infra - (2f38232) - Ashish Jaiswal
- (**hetzner**) spell out "HBMS" in user-facing strings - (68981ee) - Ashish Jaiswal
- (**hetzner**) surface YubiKey-touch prompt during HBMS reachability poll - (07e44de) - Ashish Jaiswal
- (**hetzner**) bump HBMS OS-install wait from 12 to 20 minutes - (937c0b4) - Ashish Jaiswal
- (**lint**) fix gofumpt, goconst, unparam findings - (da79bac) - Ashish Jaiswal
- (**prompt**) drop the cluster-name prefix from the worker default - (91a8680) - Ashish Jaiswal
- (**storageplan**) spell out the 2-disk requirement in the error - (b9ad412) - Ashish Jaiswal
- (**storageplan**) log every lsblk row + dropped-disk reasons - (08db3b2) - Ashish Jaiswal
- (**storageplan**) surface scanned-disk inventory on allocation failure - (16efd2e) - Ashish Jaiswal
- (**storageplan**) collapse alike layouts in the per-group display - (c4330bd) - Ashish Jaiswal

- - -

## v0.23.0 - 2026-05-18
#### Features
- (**argocd-apps**) render traefik + LE ClusterIssuer for managed Keycloak - (c886bcd) - Ashish Jaiswal
- (**argocd-apps**) conditional keycloakx + cloudnative-pg rendering - (e66215f) - Ashish Jaiswal
- (**bootstrap**) show elapsed run time in the next-steps panel title - (5e4014f) - Ashish Jaiswal
- (**bootstrap**) inline Keycloak password + OIDC-only netbird up in panel - (3a72eb5) - Ashish Jaiswal
- (**bootstrap**) add post-bootstrap next-steps panel for VPN clusters - (a34e2fb) - Ashish Jaiswal
- (**bootstrap**) gate on cert-manager Certificates being Ready - (7e87a8d) - Ashish Jaiswal
- (**bootstrap**) gate post-provisioning on CP Nodes' networking-ready - (e85ac79) - Ashish Jaiswal
- (**bootstrap**) fail fast when workload bootstrap can't reach the mesh - (2606c7f) - Ashish Jaiswal
- (**bootstrap**) workload-cluster OIDC pre-flight banner + how-to doc - (9c3aa91) - Ashish Jaiswal
- (**capi-cluster**) pass KubeaidFork.Version to chart's global.kubeaid.version - (43c10e5) - Ashish Jaiswal
- (**cli**) add login subcommand for klist-based kubeconfig generation - (40d7298) - Ashish Jaiswal
- (**config**) allow keycloak reference on workload clusters (mode=external) - (f91f5e5) - Ashish Jaiswal
- (**config**) auto-derive apiServer.oidc from cluster.keycloak when managed - (e4a426f) - Ashish Jaiswal
- (**config**) drop commit-hash kubeaid version, reject 'latest', probe remote at validate - (7604f07) - Ashish Jaiswal
- (**config**) allow cluster.keycloak.mode=external on VPN clusters - (1bd9725) - Ashish Jaiswal
- (**config**) add cluster.netbird.{stunDNS,turnDNS,turnUser} - (04bf6e7) - Ashish Jaiswal
- (**config**) require cluster.acmeEmail for managed Keycloak - (5452fd8) - Ashish Jaiswal
- (**config**) cluster.keycloak schema + validation - (f968321) - Ashish Jaiswal
- (**config**) keycloak/OIDC apiServer block + pre-bootstrap discovery probe - (1130587) - Ashish Jaiswal
- (**core**) split managed-vs-VPN-cluster gates and template lists - (23a80bd) - Ashish Jaiswal
- (**git**) GPG-sign commits when YubiKey GPG card is present - (e5e8756) - Ashish Jaiswal
- (**git**) box the PR-merge prompt with lipgloss to match the DNS table - (8b23930) - Ashish Jaiswal
- (**git**) show clickable PR URL in WaitUntilPRMerged prompt - (6100da7) - Ashish Jaiswal
- (**git**) wait for operator ENTER instead of polling for PR merge - (04fb147) - Ashish Jaiswal
- (**goreleaser**) publish standalone kubeaid-storagectl binary asset - (573c2aa) - Ashish Jaiswal
- (**hcloud**) enable deletion protection on critical HCloud resources - (1e76dff) - Rishi
- (**hcloud**) expose control-plane LB IPs for chart-side cert SAN + CoreDNS patch - (b1beb17) - Ashish Jaiswal
- (**hcloud**) implement NAT persistence and improve CCM configuration - (896255e) - Sanskar Bhushan
- (**hcloud-lb**) surface CCM SyncLoadBalancerFailed events during LB wait - (af98691) - Ashish Jaiswal
- (**hetzner**) post-Traefik DNS wait for ingress-LB FQDNs - (ab004a6) - Ashish Jaiswal
- (**hetzner**) bound DNS-wait to 5min, show timer + retry, OS resolver only - (b97c328) - Ashish Jaiswal
- (**hetzner**) render DNS-wait status as a lipgloss table - (47a8b4d) - Ashish Jaiswal
- (**hetzner**) wait for stun/turn DNS too, redraw status table in place - (058acb8) - Ashish Jaiswal
- (**hetzner**) pre-create control-plane LB for cluster.type=vpn - (207f0f3) - Ashish Jaiswal
- (**hetzner**) pause for DNS A-record propagation after LB create - (f15f0db) - Ashish Jaiswal
- (**hetzner**) automate bare metal OS install via Robot API - (2b319a4) - Shivam
- (**k8s**) add Kubernetes EOL checks and update workflow for validation - (b18d5af) - Shubham Gupta
- (**keycloak**) capitalize api/groups labels on Keycloak consent screen - (d4b1559) - Ashish Jaiswal
- (**keycloak**) add groups scope + Group Membership mapper for JWT sync - (37a3eda) - Ashish Jaiswal
- (**keycloak**) add base64-key generator and pluggable secret-fetch helper - (add7f9d) - Ashish Jaiswal
- (**keycloak**) wire ReconcileNetBird into BootstrapCluster - (17ce3a7) - Ashish Jaiswal
- (**keycloak**) add ReconcileNetBird orchestrator - (4f1f3cb) - Ashish Jaiswal
- (**keycloak**) idempotent admin-API reconciler via gocloak - (7bb8c40) - Ashish Jaiswal
- (**keycloak**) render keycloak-admin SealedSecret on managed VPN clusters - (e07105f) - Ashish Jaiswal
- (**kubeprom**) run build.sh inside a small docker image - (a010e8f) - Ashish Jaiswal
- (**login**) merge into kubeconfig, direct cluster.customer arg, suggestions on miss - (f47287a) - Ashish Jaiswal
- (**login**) NetBird-driven interactive picker + invoke kubelogin - (b4932d3) - Ashish Jaiswal
- (**netbird**) render NetBird ArgoCD app + values overlay - (6cc83d4) - Ashish Jaiswal
- (**netbird**) patch postgres DSN into netbird Secret post-CNPG-sync - (9e69bde) - Ashish Jaiswal
- (**netbird**) render full netbird + netbird-turn-credentials Secrets - (bd831e9) - Ashish Jaiswal
- (**netbird-operator**) render on VPN clusters too - (0505010) - Ashish Jaiswal
- (**oidc**) trust Obmondo's Keycloak as a second JWT issuer when monitoring is on - (68b6990) - Ashish Jaiswal
- (**oidc**) render AuthenticationConfiguration YAML instead of --oidc-* flags - (e59d144) - Ashish Jaiswal
- (**progress**) live CAPI status table during main-cluster wait - (2f6b573) - Ashish Jaiswal
- (**progress**) replace ArgoCD-access help text with lipgloss bordered box - (b4e4ab5) - Ashish Jaiswal
- (**progress**) split infra-provider sync into sync + pod-wait substeps - (7117fe1) - Ashish Jaiswal
- (**progress**) substeps across the entire bootstrap flow to the finish line - (0a4fc93) - Ashish Jaiswal
- (**progress**) substeps for cluster-api-operator + infra-provider sync - (d6c5c30) - Ashish Jaiswal
- (**progress**) show in-flight long-running steps with a transient sub-step - (dd05aa2) - Ashish Jaiswal
- (**progress**) substeps for the long-running management-cluster phase - (82c619c) - Ashish Jaiswal
- (**progress**) switch from tree-branch to section-style major-step layout - (b7ba298) - Ashish Jaiswal
- (**progress**) YubiKey prompt names the repo too ("clone Obmondo/kubeaid-config") - (1afcdc9) - Ashish Jaiswal
- (**progress**) YubiKey prompt names the op ("Tap YubiKey to <reason>") - (bbbdba3) - Ashish Jaiswal
- (**progress**) bracket all SSH-using git ops with the touch hint - (3050406) - Ashish Jaiswal
- (**progress**) tree-style sub-steps + dynamic YubiKey-touch hint - (1eb56a7) - Ashish Jaiswal
- (**progress**) docker-style log-up for completed bootstrap steps - (ee4f95f) - Ashish Jaiswal
- (**prompt**) post-prompt notice naming the 2 manual NetBird steps - (2b7dcb7) - Ashish Jaiswal
- (**prompt**) replace workload OIDC form with Keycloak-ref + reachability probe - (3adc922) - Ashish Jaiswal
- (**prompt**) auto-keyscan SSH host keys for self-hosted git forges - (1601540) - Ashish Jaiswal
- (**prompt**) hide Hetzner SSH key path when agent is detected - (68dab8d) - Ashish Jaiswal
- (**prompt**) source SSH key pair via agent when UseSSHAgent=true - (4d8b115) - Ashish Jaiswal
- (**prompt**) add Step 0 K8s version profile picker - (14cdb8f) - Ashish Jaiswal
- (**prompt**) redesign bootstrap prompt UX with grouped huh forms - (ac21d10) - Ashish Jaiswal
- (**prompt**) auto-derive NetBird/CP/ACME defaults from Keycloak DNS - (cefd838) - Ashish Jaiswal
- (**prompt**) ask Keycloak mode, collect netbird-backend secret on external - (699bab7) - Ashish Jaiswal
- (**prompt**) collect VPN cluster details (Keycloak/NetBird/ACME) - (ff1021b) - Ashish Jaiswal
- (**prompt**) ask whether to enable OIDC during interactive setup - (384dd49) - Ashish Jaiswal
- (**render**) netbird-operator ArgoCD app for workload + keycloak clusters - (1c5c617) - Ashish Jaiswal
- (**secrets**) idempotent sealed-secret rendering + managed-by label - (e2dc7dd) - Ashish Jaiswal
- adding bucket B unit tests - (7083e0c) - lucaspirito
#### Bug Fixes
- (**argocd**) claim the sealed-secrets controller resources right after root sync - (fdeb81f) - Ashish Jaiswal
- (**argocd**) wait on root app's reconcile status, not repo-server Deployment - (4982900) - Ashish Jaiswal
- (**argocd-sync**) sync cloudnative-pg before keycloakx and netbird - (b64ab01) - Ashish Jaiswal
- (**argocd-sync**) hard-refresh + retry on transient repo-fetch failures - (fc2dd17) - Ashish Jaiswal
- (**bootstrap**) widen Keycloak reconcile retry budget to ~60s - (d36fad4) - Ashish Jaiswal
- (**bootstrap**) include /auth in Keycloak base URL - (95b2f5e) - Ashish Jaiswal
- (**bootstrap**) use Keycloak public URL for admin reconcile, drop port-forward - (22a4d8d) - Ashish Jaiswal
- (**bootstrap**) retry Keycloak admin reconcile on transient EOFs - (5d6514b) - Ashish Jaiswal
- (**bootstrap**) create Keycloak OIDC clients before netbird syncs - (4e32a89) - Ashish Jaiswal
- (**bootstrap**) make the TLS cert timeout error readable - (e69200f) - Ashish Jaiswal
- (**bootstrap**) pause the progress bar around the ArgoCD-dashboard box - (be607a0) - Ashish Jaiswal
- (**bootstrap**) consolidate the pre-pivot output into one block - (e695ce0) - Ashish Jaiswal
- (**bootstrap**) sync ccm + traefik before the ingress-LB-DNS gate - (55a8252) - Ashish Jaiswal
- (**bootstrap**) narrow root sync on mgmt cluster to mgmt-relevant child Apps - (427076c) - Ashish Jaiswal
- (**bootstrap**) wait for Machines + render Nodes table before clusterctl move - (e387513) - Ashish Jaiswal
- (**bootstrap**) drop wasteful 5-minute pre-recovery wait on sealed-secrets - (c104eb5) - Ashish Jaiswal
- (**bootstrap**) verify sealed-secrets functional via API state, not Helm record - (d69987f) - Ashish Jaiswal
- (**bootstrap**) pre-seed sealed-secrets keys before install + secrets-app-first sync - (8629b31) - Ashish Jaiswal
- (**bootstrap**) copy sealed-secrets keys from management to main cluster - (45c2f81) - Ashish Jaiswal
- (**bootstrap**) pre-apply kube-system/cloud-credentials so HCloud CCM can start - (cb0c00c) - Ashish Jaiswal
- (**bootstrap**) autoscaler sync — AWS/Azure workload only, drop the intermediate var - (24457e9) - Ashish Jaiswal
- (**bootstrap**) skip cluster-autoscaler sync on Hetzner - (3223d19) - Ashish Jaiswal
- (**capi-cluster**) restore extraCertSANs + loadBalancer at controlPlane level - (23199ad) - Ashish Jaiswal
- (**capi-cluster**) render only chart-schema-permitted fields under controlPlane - (76d115f) - Ashish Jaiswal
- (**ccm-hcloud**) target LB backends by private IP on vpn-type clusters - (a353c67) - Ashish Jaiswal
- (**cli**) skip config prompt + container proxy on bare 'cluster' / 'devenv' - (f3f54b2) - Ashish Jaiswal
- (**cloud-credentials**) include network key for VPN-cluster CCM networking - (7e831e8) - Ashish Jaiswal
- (**commandexecutor**) readable error when a sub-command exits non-zero - (be83125) - Ashish Jaiswal
- (**config**) drop duplicate useSSHAgent field on GitConfig - (6fb29bc) - Ashish Jaiswal
- (**git**) drop redundant post-push default-branch lookup - (b9448e8) - Ashish Jaiswal
- (**git**) skip empty-commit-push-merge dance when kubeaid-config has no changes - (df4cbec) - Ashish Jaiswal
- (**git**) rebrand kubeaid-cli's commit identity from "KubeAid Bootstrap Script" to "Obmondo" - (c4e77fe) - Ashish Jaiswal
- (**git**) use 'gpg --card-status' for smartcard detection - (cce62e9) - Ashish Jaiswal
- (**git**) scope PR-merge verify fetch to default branch only, force-update - (6e56b80) - Ashish Jaiswal
- (**git**) suppress YubiKey-touch hint for HTTPS-routed transport ops - (3a917ef) - Ashish Jaiswal
- (**git**) align PR-merge prompt to the substep tree column - (506fd82) - Ashish Jaiswal
- (**git-auth**) route agent-only configs correctly + show YubiKey touch hint - (5f1b694) - Ashish Jaiswal
- (**giturl**) strip SSH port from HTTPCloneURL for ssh-derived URLs - (b16a66d) - Ashish Jaiswal
- (**giturl**) strip port from Host for filesystem paths and TLS SAN - (1cd5075) - Ashish Jaiswal
- (**hcloud-lb**) disable control-plane LB public IP on VPN clusters too - (527633c) - Ashish Jaiswal
- (**helm**) switch sealed-secrets recovery from Install.Replace to Upgrade - (07b4b4f) - Ashish Jaiswal
- (**helm**) use Install.Replace=true to recover stuck releases instead of uninstall - (8c1d8e9) - Ashish Jaiswal
- (**hetzner**) accept HCloud's 201 from ChangeProtection alongside 200 - (c84f893) - Ashish Jaiswal
- (**hetzner**) drop dead multipleSubnets config field - (ac52938) - Ashish Jaiswal
- (**hetzner**) NAT gateway PrivateNet panic + LB service/target wiring - (9affbaa) - Ashish Jaiswal
- (**hetzner**) narrow pre-cluster DNS wait to control-plane hostname only - (119ab94) - Ashish Jaiswal
- (**hetzner**) create NAT gateway before LB+DNS-wait, not after - (ed3dd31) - Ashish Jaiswal
- (**hetzner**) hoist Ctrl+C onto timer line, drop redundant verified message - (53386ef) - Ashish Jaiswal
- (**hetzner**) move Ctrl+C hint below DNS status table, match PR-merge box - (ec7ec16) - Ashish Jaiswal
- (**hetzner**) drop DNS-wait skip option, tidy the header line - (54d5a6e) - Ashish Jaiswal
- (**hetzner**) render DNS-status table immediately, before first lookups - (047c409) - Ashish Jaiswal
- (**hetzner**) try all ARM-capable HCloud locations for NAT gateway placement - (2daf749) - Ashish Jaiswal
- (**hetzner**) run HBMS OS install in pure bare-metal mode - (a3065fc) - Shivam
- (**keycloak**) prepend /auth to every derived Keycloak issuer URL - (145cabb) - Ashish Jaiswal
- (**keycloak**) set audience mapper to netbird-client client_id - (1255dd0) - Ashish Jaiswal
- (**keycloak**) enable OAuth 2.0 Device Authorization Grant on netbird-client - (c348c8c) - Ashish Jaiswal
- (**keycloak**) wire keycloakx ingress for cert-manager TLS - (d6ea1ab) - Ashish Jaiswal
- (**kube-prom-builder**) render templates before running build.sh - (0213e1f) - Ashish Jaiswal
- (**kube-prom-builder**) drop the redundant ImagePull, image is built locally - (bd370c2) - Ashish Jaiswal
- (**kube-prom-builder**) multi-stage build, drop ~552 MB of go toolchain from final image - (edfc7d7) - Ashish Jaiswal
- (**lint**) handle os.RemoveAll error + cut CreateNATGateway complexity - (db286c0) - Ashish Jaiswal
- (**lint**) address golangci-lint findings from prior PRs - (67326f5) - Ashish Jaiswal
- (**login**) clearer errors and explicit-empty NetBird filter - (ddee462) - Ashish Jaiswal
- (**netbird**) prefix Keycloak URLs with /auth in NetBird values - (0542e0f) - Ashish Jaiswal
- (**netbird**) pin postgresql backups off in kubeaid-cli's values overlay - (6e95944) - Ashish Jaiswal
- (**oidc**) skip pre-bootstrap discovery probe when Keycloak is managed - (157f997) - Ashish Jaiswal
- (**pr**) keep merge-prompt URL on one line so click capture works - (5359295) - Ashish Jaiswal
- (**progress**) PR-merge box now wraps long URLs inside the border - (ad167e8) - Ashish Jaiswal
- (**progress**) redraw survives mid-render terminal resize - (4728ae2) - Ashish Jaiswal
- (**progress**) redraw CAPI live-status timer line every second - (f95b6fb) - Ashish Jaiswal
- (**progress**) drop redundant "(on management)" suffix from substep - (0bd8fd1) - Ashish Jaiswal
- (**progress**) port line-count redraw to DNS-wait loop - (a63280c) - Ashish Jaiswal
- (**progress**) keep PR-merge box within terminal width on long URLs - (5293455) - Ashish Jaiswal
- (**progress**) move capi-cluster sync substep under "Creating management cluster" - (f23b8f6) - Ashish Jaiswal
- (**progress**) wait for the right infra-provider pod, on a sane interval - (fb54b81) - Ashish Jaiswal
- (**progress**) pause spinner during PR-merge prompt, auto-hide on success - (059969f) - Ashish Jaiswal
- (**progress**) erase YubiKey touch indicator on release instead of leaving Touched ✓ line - (753140c) - Ashish Jaiswal
- (**progress**) drop redundant major-step name from spinner caption - (ec73cba) - Ashish Jaiswal
- (**progress**) print "::  <major>" header so sub-steps are anchored - (e13ed75) - Ashish Jaiswal
- (**progress**) skip OIDC validation step for managed-Keycloak too - (9db32d2) - Ashish Jaiswal
- (**progress**) skip "Validating OIDC issuer" step when OIDC is off - (3a0d0ad) - Ashish Jaiswal
- (**prompt**) drop empty detection panels, inline KubeAid tag in picker title - (04e3c56) - Ashish Jaiswal
- (**prompt**) default kubeaid-config fork URL to SSH form, clarify key labels - (f608f36) - Ashish Jaiswal
- (**prompt**) default kubeaid + kubeaid-config fork URLs to HTTPS - (b93fbf4) - Ashish Jaiswal
- (**sealed-secrets**) fold controller cert into render cache key - (b694c39) - Ashish Jaiswal
- (**sync**) re-issue ArgoCD Sync in poll loop, not just hard-refresh - (1ce7ff8) - Ashish Jaiswal
- (**sync**) trust App.spec.syncPolicy.syncOptions, drop per-request override - (66c849d) - Ashish Jaiswal
- (**templates**) cluster-api-operator app uses KubeAid fork version - (fc1e54a) - Ashish Jaiswal
- (**templates**) eliminate trailing whitespace in rendered files - (6b55a2a) - Ashish Jaiswal
- (**test**) TestBuildPostgresDSN subtests should call t.Parallel - (336f5a0) - Ashish Jaiswal
- (**traefik**) send PROXY header from HCloud LB to preserve client IP - (7e5715a) - Ashish Jaiswal
- using util call for getenv - (ea5d6e1) - lpirito
- removing unnecessary aws dep for string pointer - (eb0dbe0) - lpirito
- waiting for apps to be ready instead of fixed sleep to request apps - (6bc73f4) - lpirito
- added termsetup package to prevent RGB errors in Docker PTY - (2bf1156) - Shubham Gupta
- trivy scan configuration and ignore some CVEs with fixing the Dockerfile to run as non-root user - (271374b) - Shubham Gupta
- Ubuntu validation, naming abbreviations - (40eb9af) - Shivam
#### Performance Improvements
- (**git**) drop redundant fetch in CreateAndCheckoutToBranch - (572b50e) - Ashish Jaiswal
- (**git**) cache origin/HEAD locally so GetDefaultBranchName skips remote list-refs - (f77218a) - Ashish Jaiswal
#### Documentation
- (**config**) regenerate config-reference.md + sample yaml for OIDC block - (94f132b) - Ashish Jaiswal
- (**git**) TODO to wire runtime passphrase prompt for encrypted private keys - (f3037e8) - Ashish Jaiswal
- (**keycloak**) cover external mode in keycloak-bootstrap.md - (4ad3775) - Ashish Jaiswal
- (**post-bootstrap**) add break-glass / disaster-recovery section - (3ab78a1) - Ashish Jaiswal
- (**progress**) clarify infra-provider wait works across all CAPI providers - (f72b66e) - Ashish Jaiswal
- add post-bootstrap operator guide - (a593fc7) - Ashish Jaiswal
- TODO list + VPN cluster network diagram - (9af959a) - Ashish Jaiswal
- add keycloak bootstrap design doc - (6725106) - Ashish Jaiswal
- add Hetzner k8s with NetBird-gated kube-api architecture - (6119bdd) - Ashish Jaiswal
- add architecture and configuration guide for Hetzner HCloud VPN cluster - (82930bb) - Sanskar Bhushan
#### Tests
- (**aws**) add unit tests for IAM and S3 services - (94b6d36) - Shubham Gupta
- (**aws**) add unit tests for cloud operations - (e2789a6) - Shubham Gupta
- (**aws**) add service fakes for IAM and S3 - (85596aa) - Shubham Gupta
- (**azure**) add unit tests for cloud operations - (f2b2643) - Shubham Gupta
- (**git**) unit-test gitAuthModeFor routing for both auth paths - (d7631a8) - Ashish Jaiswal
- (**hetzner**) add unit tests for loadbalancer operations - (e14f02c) - Shubham Gupta
- (**hetzner**) add unit tests for cloud operations - (7b87131) - Shubham Gupta
- (**keycloak**) add nolint directive and parallel subtest - (269d3d4) - Shubham Gupta
- (**prompt**) cover deriveRealmFromDNS / stripFirstLabel / deriveACMEEmailFromDNS - (2488689) - Ashish Jaiswal
- (**sealed-secrets**) pin cert-folded cache invariants for SealIfPlaintextChanged - (f7e7aeb) - Ashish Jaiswal
- add coverage for the post-managed-Keycloak code paths - (0453e6e) - Ashish Jaiswal
- add test to validate os installation - (5da738f) - Shivam
- add Bucket A unit tests - (bc03c78) - Shivam
- add unit tests for Kubernets utilities - (21eb270) - Shubham Gupta
#### Refactoring
- (**bootstrap**) unify ordered app sync into one []AppSyncStep list - (ccbf92b) - Ashish Jaiswal
- (**bootstrap**) gate per-app on the TLS Certificate, not after the whole sync - (67232f1) - Ashish Jaiswal
- (**cloud**) return errors from CloudProvider interface and implementations - (94ad81b) - Shubham Gupta
- (**core**) use docker SDK for kube-prometheus build, drop shell-out - (03bc141) - Shubham Gupta
- (**core**) use docker SDK for kube-prom-builder image build too - (5d34b29) - Ashish Jaiswal
- (**core**) refactor KubePrometheus build process to use Docker client - (d8a49fb) - Shubham Gupta
- (**git**) narrow per-repo fetch + attribute commits to operator - (4ff7c9d) - Ashish Jaiswal
- (**hcloud**) rename loadBalancer.hostname to endpoint and require it - (fab13aa) - Ashish Jaiswal
- (**hetzner**) make loadbalancer.go testable with dependency injection - (a6f1f79) - Shubham Gupta
- (**hetzner**) pin HBMS install to latest Ubuntu LTS - (3e73ce0) - Shivam
- (**keycloak**) extract kubernetes-<cluster> OIDC client to its own file - (d7d4505) - Ashish Jaiswal
- (**netbird**) move CNPG DSN patch into netbird's AfterSync hook - (429de56) - Ashish Jaiswal
- (**progress**) YubiKey touch as a transient sub-step instead of spinner suffix - (d868664) - Ashish Jaiswal
- (**progress**) single "✓ <step>" header per major step + sub-steps under management cluster - (730499f) - Ashish Jaiswal
- (**progress**) fold YubiKey-touch hint into the spinner caption - (588ae04) - Ashish Jaiswal
- (**secrets**) persist auto-generated values in secrets.yaml - (7123dc5) - Ashish Jaiswal
- drop kubescape/go-git-url for self-hosted forge support - (5f5e67f) - Ashish Jaiswal
- remove docker and related files for single binary approach - (af26b61) - Shubham Gupta
#### Miscellaneous Chores
- (**config**) drop stale HetznerConfig.VPNCluster duplicate field - (c3c7462) - Ashish Jaiswal
- (**dns-wait**) bump total timeout 5min → 10min - (ce01247) - Ashish Jaiswal
- (**lint**) clear all golangci-lint issues - (9b07994) - Ashish Jaiswal
- (**login**) satisfy golangci-lint (copyloopvar, errcheck, gofumpt, gosec) - (7be146e) - Ashish Jaiswal
- (**make**) collapse build alias, rename build-kubeaid-storagectl - (7b05810) - Ashish Jaiswal
- argocd retry on error - (6808e49) - Shubham Gupta
- fix lint and test - (31967f2) - Shubham Gupta
- update test coverage thresholds for cloud packages - (82628d7) - Shubham Gupta
#### Style
- lowercase error messages per Go conventions - (32b7563) - Shubham Gupta

- - -

## v0.22.4 - 2026-04-28
#### Bug Fixes
- (**kubeaid-agent**) create ArgoCD project-role Secret in obmondo namespace - (ffb9cc3) - Ashish Jaiswal
#### Miscellaneous Chores
- (**ci**) add ci to validate bad commit message - (7ddfd73) - Sanskar Bhushan

- - -

## v0.22.3 - 2026-04-24
#### Bug Fixes
- (**monitoring**) render alertmanager-main Secret to prevent pod deadlock - (57c5009) - Ashish Jaiswal

- - -

## v0.22.2 - 2026-04-23
#### Bug Fixes
- (**argocd**) render extraHosts under argo-cd subchart prefix - (8ad8124) - Ashish Jaiswal
- (**obmondo-clientcert**) render with base64Encode, not b64enc - (7789813) - Ashish Jaiswal
- create the namespace for monitoring, to apply the secret before hand - (74c6fc6) - Ashish Jaiswal
#### Refactoring
- migrate from survey to huh for interactive prompts and improve input handling - (297bbfb) - Shubham Gupta

- - -

## v0.22.1 - 2026-04-23
#### Bug Fixes
- (**ci**) pin trivy-action to v0.36.0 (existing tag) - (f27be6d) - Ashish Jaiswal
- (**ci**) pin actions/setup-go to go.mod so goreleaser finds go - (954abd1) - Ashish Jaiswal
- (**cog**) push main alongside tag, and restrict bumps to main - (e2dbc55) - Ashish Jaiswal
- (**gitea**) restore Set up Go step for goreleaser - (362b20e) - Ashish Jaiswal

- - -

## v0.22.0 - 2026-04-23
#### Features
- (**e2e**) wire obmondo.monitoring to deploy kubeaid-agent via ArgoCD - (e222cd9) - Ashish Jaiswal
- (**obmondo**) derive kube-prometheus certname from the mTLS cert CN - (3f5ec55) - Ashish Jaiswal
- (**obmondo**) require mTLS cert+key when monitoring is enabled - (37afacc) - Ashish Jaiswal
- Add interactive prompting for cluster configuration - (1da5422) - Shubham Gupta
#### Bug Fixes
- (**argocd**) reuse rendered values-argocd.yaml for initial ArgoCD install - (12436c4) - Ashish Jaiswal
- (**ci**) allow different binary count for goreleaser archive - (00249e1) - Archisman
- (**config**) validate git.knownHosts entries before bootstrap - (7b55391) - Ashish Jaiswal
- (**config**) add obmondo.teleportAgent toggle and teleportAuthToken secret - (da64e7b) - Ashish Jaiswal
- (**generators**) satisfy CreateLogger's two-writer contract - (1c1cc55) - Ashish Jaiswal
- (**gitea**) keep cog's release notes out of goreleaser's workspace - (96290e7) - Ashish Jaiswal
- (**obmondo**) render teleport-kube-agent with join token end-to-end - (7868294) - Ashish Jaiswal
- (**obmondo**) render mTLS client cert as a sealed-secret in both namespaces - (79f6879) - Ashish Jaiswal
- (**root**) update log file permissions to 0644 - (3c436a5) - Shubham Gupta
- (**storageplanner**) assigning priority score to disks for ZFS installation - (29e771d) - Archisman
- (**templates**) split teleport-kube-agent so it can be gated independently - (788eda9) - Ashish Jaiswal
#### Documentation
- (**obmondo**) regenerate sample general.yaml + config-reference - (3406154) - Ashish Jaiswal
- (**release**) document cog-driven release flow - (a8af02e) - Ashish Jaiswal
- add Architecture.md - (b309b45) - Shivam
#### Tests
- (**argocd**) cover argoCDHelmValues presence and absence of values-argocd.yaml - (ec58393) - Ashish Jaiswal
#### Continuous Integration
- (**gitea**) strip goreleaser to changelog-only - (c48aefb) - Ashish Jaiswal
- (**gitea**) generate release notes via cog and attach full CHANGELOG - (aafd9f7) - Ashish Jaiswal
- (**release**) build KubeAid Core natively per arch instead of QEMU - (d80de06) - Ashish Jaiswal
- split goreleaser config into github and gitea variants - (6e99810) - Ashish Jaiswal
#### Miscellaneous Chores
- (**ci**) remove building binary and container image on every PR - (d858ca9) - Archisman
- (**cog**) push release tags to both origin (gitea) and github - (b1bbe4f) - Ashish Jaiswal
- (**version**) v0.22.0 - (256dcff) - Ashish Jaiswal
- (**version**) v0.21.0 - (4adaada) - Archisman

- - -

## v0.22.0 - 2026-04-23
#### Features
- (**e2e**) wire obmondo.monitoring to deploy kubeaid-agent via ArgoCD - (e222cd9) - Ashish Jaiswal
- (**obmondo**) derive kube-prometheus certname from the mTLS cert CN - (3f5ec55) - Ashish Jaiswal
- (**obmondo**) require mTLS cert+key when monitoring is enabled - (37afacc) - Ashish Jaiswal
- Add interactive prompting for cluster configuration - (1da5422) - Shubham Gupta
#### Bug Fixes
- (**argocd**) reuse rendered values-argocd.yaml for initial ArgoCD install - (12436c4) - Ashish Jaiswal
- (**ci**) allow different binary count for goreleaser archive - (00249e1) - Archisman
- (**config**) validate git.knownHosts entries before bootstrap - (7b55391) - Ashish Jaiswal
- (**config**) add obmondo.teleportAgent toggle and teleportAuthToken secret - (da64e7b) - Ashish Jaiswal
- (**generators**) satisfy CreateLogger's two-writer contract - (1c1cc55) - Ashish Jaiswal
- (**obmondo**) render teleport-kube-agent with join token end-to-end - (7868294) - Ashish Jaiswal
- (**obmondo**) render mTLS client cert as a sealed-secret in both namespaces - (79f6879) - Ashish Jaiswal
- (**root**) update log file permissions to 0644 - (3c436a5) - Shubham Gupta
- (**storageplanner**) assigning priority score to disks for ZFS installation - (29e771d) - Archisman
- (**templates**) split teleport-kube-agent so it can be gated independently - (788eda9) - Ashish Jaiswal
#### Documentation
- (**obmondo**) regenerate sample general.yaml + config-reference - (3406154) - Ashish Jaiswal
- (**release**) document cog-driven release flow - (a8af02e) - Ashish Jaiswal
- add Architecture.md - (b309b45) - Shivam
#### Tests
- (**argocd**) cover argoCDHelmValues presence and absence of values-argocd.yaml - (ec58393) - Ashish Jaiswal
#### Continuous Integration
- (**gitea**) strip goreleaser to changelog-only - (c48aefb) - Ashish Jaiswal
- (**gitea**) generate release notes via cog and attach full CHANGELOG - (aafd9f7) - Ashish Jaiswal
- (**release**) build KubeAid Core natively per arch instead of QEMU - (d80de06) - Ashish Jaiswal
- split goreleaser config into github and gitea variants - (6e99810) - Ashish Jaiswal
#### Miscellaneous Chores
- (**ci**) remove building binary and container image on every PR - (d858ca9) - Archisman
- (**cog**) push release tags to both origin (gitea) and github - (b1bbe4f) - Ashish Jaiswal
- (**version**) v0.21.0 - (4adaada) - Archisman

- - -

## v0.21.0 - 2026-04-16
#### Features
- (**ci**) add build binaries and Docker image steps to CI workflow - (cc48157) - Shubham Gupta
- (**hetzner**) adding support for Hetzner Network - (c2b4b25) - Archisman
- (**hetzner/bare-metal**) storage plan generation and execution using kubeaid-storagectl - (d73ce0c) - Archisman
- (**hetzner/bare-metal**) automatically choosing disks for OS, ZFS and CEPH - (5a1c63c) - Archisman
#### Bug Fixes
- (**bare-metal**) creating the Kubeone config file before provisioning the main cluster - (915ab45) - Archisman
- (**ci**) allow different binary count for goreleaser archive - (00249e1) - Archisman
- (**ci**) separate ids for KubeAid CLI and KubeAid StorageCTL binary builds in GoReleaser config file - (9cae292) - Archisman
- (**ci**) shell command to download the JSONNET binary - (0867426) - Archisman
- (**cli**) initialize KubeAidCoreContainer fields before Run() - (8732264) - Sanskar Bhushan
- (**generator**) ignoring struct fields which aren't considered during YAML unmarshalling - (935d0b9) - Archisman
- (**hetzner**) removing G and passing OS and ZFS pool size as integers to capi-cluster Helm chart - (b8f4c2d) - Archisman
- (**storageplanner**) assigning priority score to disks for ZFS installation - (29e771d) - Archisman
- (**validation/kube-prometheus**) not supporting KubePrometheus v0.15 - (6c9963c) - Archisman
- correct InitTempDir exists-check, WithRetry zero-guard, and AssignPriorityScores NIC bonus - (04caa91) - Shivam
- all the bugs and run the local cluster - (d3fb0d6) - Shubham Gupta
- auto-add disk=nvme label to nodegroups with nvme zfs disks - (19db184) - mavrick-1
- waiting for lb ip provision and sealed-secret condition - (48b185d) - lucaspirito
- KubePrometheus version validation - (9d4310c) - Archisman
- bugs caught when trying to provision Kilroy's QA cluster - (da6728f) - Archisman
- correct install.sh URL path in README - (56371a3) - mavrick-1
- PR workflow by upgrading GoLangCI Lint version - (8745299) - Archisman
- issues with PR 353, and, simplify SSH key-pair processing logic - (2c6efd2) - Archisman
- pkg/cloud/hetzner/ssh_key.go - (c0eaee1) - Archisman
#### Tests
- add unit tests for utils and storageplanner packages - (f24623a) - Shivam
- bump test coverage for modules - (1333a69) - Shivam
#### Continuous Integration
- replacing commitlint + standard-version with cocogitto - (7f9d550) - Archisman
#### Refactoring
- move disk=nvme label hydration out of GenerateStoragePlans - (2b032e2) - mavrick-1
#### Miscellaneous Chores
- (**ci**) remove building binary and container image on every PR - (d858ca9) - Archisman
- (**dev**) shifting from commitlint to conform - (9a27376) - Archisman
- (**hetzner**) ignoring StoragePlan field from YAML encoding - (0e7c2fd) - Archisman
- (**version**) v0.21.0 - (5e5dd0e) - Archisman
- fixes and improvements related to vibe coded changes - (a4ab503) - Archisman
- pull from main branch and resolve merge conflicts - (912d609) - Archisman
- not streaming local command execution output in most of the cases - (a6a8021) - Archisman
- moving install.sh inside the scripts folder - (cf96cb4) - Archisman
- pulling from the main branch and resolving merge conflicts - (40a7f95) - Archisman
- pull from main branch and resolve merge conflicts - (27464a0) - Archisman

- - -

