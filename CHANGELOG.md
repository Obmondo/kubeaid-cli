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

