- - -
## v0.21.0 - 2026-04-13
#### Features
- (**ci**) add build binaries and Docker image steps to CI workflow - (cc48157) - Shubham Gupta
- (**hetzner**) adding support for Hetzner Network - (c2b4b25) - Archisman
- (**hetzner/bare-metal**) storage plan generation and execution using kubeaid-storagectl - (d73ce0c) - Archisman
- (**hetzner/bare-metal**) automatically choosing disks for OS, ZFS and CEPH - (5a1c63c) - Archisman
#### Bug Fixes
- (**bare-metal**) creating the Kubeone config file before provisioning the main cluster - (915ab45) - Archisman
- (**ci**) separate ids for KubeAid CLI and KubeAid StorageCTL binary builds in GoReleaser config file - (9cae292) - Archisman
- (**ci**) shell command to download the JSONNET binary - (0867426) - Archisman
- (**cli**) initialize KubeAidCoreContainer fields before Run() - (8732264) - Sanskar Bhushan
- (**generator**) ignoring struct fields which aren't considered during YAML unmarshalling - (935d0b9) - Archisman
- (**hetzner**) removing G and passing OS and ZFS pool size as integers to capi-cluster Helm chart - (b8f4c2d) - Archisman
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
- (**dev**) shifting from commitlint to conform - (9a27376) - Archisman
- (**hetzner**) ignoring StoragePlan field from YAML encoding - (0e7c2fd) - Archisman
- fixes and improvements related to vibe coded changes - (a4ab503) - Archisman
- pull from main branch and resolve merge conflicts - (912d609) - Archisman
- not streaming local command execution output in most of the cases - (a6a8021) - Archisman
- moving install.sh inside the scripts folder - (cf96cb4) - Archisman
- pulling from the main branch and resolving merge conflicts - (40a7f95) - Archisman
- pull from main branch and resolve merge conflicts - (27464a0) - Archisman

- - -

