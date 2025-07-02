# KubeAid Bootstrap Script

The `KubeAid Bootstrap Script` is used to bootstrap Kubernetes clusters using Cluster API and [KubeAid](https://github.com/Obmondo/KubeAid).

> Currently it only supports bootstrapping self-managed clusters in AWS.

## Official Guides

- [Bootstrapping a self-managed cluster in AWS](https://github.com/Obmondo/KubeAid/blob/master/docs/aws/capi/cluster.md)

## Developer Guide (AWS edition)

> Make sure, you've Docker installed and running in your system.

Run `make build-image-dev` to build the KubeAid Bootstrap Script container image (development version).

Then run `make run-container-dev` to run the container.

Use `make exec-container-dev` to execute into the container.

Once you're inside the container, use `make generate-sample-config-aws-dev` to generate a sample config file at [./outputs/kubeaid-bootstrap-script.config.yaml](./outputs/kubeaid-bootstrap-script.config.yaml), targetting the AWS cloud provider. Adjust the config file according to your needs.

Export your AWS credentials as environment variables like such :

```sh
export AWS_REGION=""
export AWS_ACCESS_KEY_ID=""
export AWS_SECRET_ACCESS_KEY=""
export AWS_SESSION_TOKEN=""
```

Then run `make bootstrap-cluster-dev-aws` to bootstrap the cluster!

> [!NOTE]
> If the `clusterawsadm bootstrap iam create-cloudformation-stack` command errors out with this message :
>
>      the IAM CloudFormation Stack create / update failed and it's currently in a `ROLLBACK_COMPLETE` state
>
> then that means maybe there are pre-existing IAM resources with overlapping name. Then first delete them manually from the AWS Console and then retry running the script. Filter the IAM roles and policies in the corresponding region with the keyword : `cluster` / `clusterapi`.

If cluster provisioning gets stuck, then debug by :

- checking logs of ClusterAPI related pod.

- SSHing into the control-plane node. You can view cloud-init output logs stored at `/var/log/cloud-init-output.log`.

If you want to delete the provisioned cluster, then execute : `make delete-provisioned-cluster-dev-aws`.

## Developer Guide (Running locally)

- Spin up the gitea containe using the docker compose file added in `./e2e/compose/docker-compose.yaml`

 ```bash
 cd ./e2e/compose
 docker compose up -d
 ```

- create the `general.yaml` and `secrets.yaml` config files in `./outputs/configs/local`

```bash
touch ./outputs/configs/local/general.yaml
touch ./outputs/configs/local/secrets.yaml
```

- add the below configs in `./outputs/configs/local/general.yaml` and `./outputs/configs/local/secrets.yaml` respectively for the bootstrap script to clone the repos and spin up k3d.

```yaml
forkURLs:
  kubeaid: https://enableitdk-gitea:3001/test/KubeAid
  kubeaidConfig: https://enableitdk-gitea:3001/test/kubeaid-config

cluster:
  name: kubeaid-demo-local
  k8sVersion: v1.31.0
  kubeaidVersion: 13.0.0 # update this accordingly

cloud:
  local: {}
```

```yaml
git:
  username: test
  password: password
  caCertPath: /home/ananth/go/src/gitea.obmondo.com/kubeaid-bootstrap-script/certs/custom-rootCA.pem # change this to match your local path
```

**NOTE** - The current gitea compose file in `./e2e/compose/` uses custom CA certs added in `./certs`. In case you don't want to use the customCA for your local gitea, update the compose file accordingly and keep `caCertPath` in `secrets.yaml` empty.

- run the below command to add `enableitdk-gitea` in your local `/etc/hosts`

```bash
echo "127.0.0.1 enableitdk-gitea" >> /etc/hosts
```

- Install the necessary pre-requisites

```bash
sudo chmod 777 ./scripts/install-prerequisites.sh
./scripts/install-prerequisites.sh
```

- Now run the script locally

```bash
make bootstrap-cluster-local-dev
```

## TODOs

- [ ] Check Git URL if SSH agent is used.
- [ ] Validation for sshagentauth (should not accept https url).
- [ ] Support using HTTPS for ArgoCD apps.
- [x] Support scale to / from zero for the node-groups.
  > Currently, I have added extra ClusterRole and ClusterRoleBinding in the KubeAid [cluster-autoscaler Helm chart](https://github.com/Obmondo/kubeaid/tree/master/argocd-helm-charts/cluster-autoscaler) to support this feature.
  > But I have also opened an issue in the kubernetes-sigs/autoscaler repository regarding this : [Allow adding extra rules to the Role / ClusterRole template of the Cluster AutoScaler Helm chart](https://github.com/kubernetes/autoscaler/issues/7680).

## REFERENCES

- [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/#comparison-with-client-side-apply)

- [The definitive guide to building Golang based CLI](https://www.youtube.com/watch?v=SSRIn5DAmyw)

- [AWS S3 Sync Command – Guide with Examples](https://spacelift.io/blog/aws-s3-sync)

- How KubeAid backs up Sealed Secrets using a CRONJob : <https://github.com/Obmondo/kubeaid/blob/master/argocd-helm-charts/sealed-secrets/templates/configmap.yaml>

- [Key Management](https://playbook.stakater.com/content/workshop/sealed-secrets/management.html)

- [Secret Rotation](https://github.com/bitnami-labs/sealed-secrets?tab=readme-ov-file#secret-rotation)

- [Kubernetes Backups, Upgrades, Migrations - with Velero](https://youtu.be/zybLTQER0yY?si=qOZcizBqPOeouJ7y)

- [Failover](https://docs.hetzner.com/robot/dedicated-server/ip/failover/)

- [Auditing](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/)

- [Kube API server args](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-apiserver/)

- [Using IAM roles in management cluster instead of AWS credentials](https://cluster-api-aws.sigs.k8s.io/topics/using-iam-roles-in-mgmt-cluster)

- [KubeadmControlPlane CRD](https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml)

- [How can you call a helm 'helper' template from a subchart with the correct context?](https://stackoverflow.com/questions/47791971/how-can-you-call-a-helm-helper-template-from-a-subchart-with-the-correct-conte)

- [IRSA for non EKS Clusters | PlatformCon 2023](https://www.youtube.com/watch?v=otmLHWW3Tos)

- [Logging in Go with Slog: The Ultimate Guide](https://betterstack.com/community/guides/logging/logging-in-go/)

- [Velero plugins for Microsoft Azure : Use Azure AD Workload Identity](https://github.com/vmware-tanzu/velero-plugin-for-microsoft-azure/blob/main/README.md#option-3-use-azure-ad-workload-identity)

- [Azure Workload Identity authentication with AD fails](https://github.com/vmware-tanzu/velero/issues/8324)

- [Native Routing on Kubernetes with BGP](https://ardaxi.com/blog/k8s-bgp/)

- [Play with Cilium native routing in Kind cluster](https://medium.com/@nahelou.j/play-with-cilium-native-routing-in-kind-cluster-5a9e586a81ca)

- [Cilium BGP Control Plane](https://github.com/cilium/cilium/blob/main/pkg/bgpv1/README.md)

- [Golden config for golangci-lint](https://gist.github.com/maratori/47a4d00457a92aa426dbd48a18776322)

- [ncdmv's flake.nix file](https://github.com/aksiksi/ncdmv/blob/main/flake.nix)

- [Uploading a SARIF file to GitHub](https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/uploading-a-sarif-file-to-github)

- [What is CA bundle?](https://www.namecheap.com/support/knowledgebase/article.aspx/986/69/what-is-ca-bundle/)

- [What is RAID 0, 1, 5, & 10?](https://www.youtube.com/watch?v=U-OCdTeZLac)

- [What is RAID Parity?](https://www.youtube.com/watch?v=BjuBloMHhKk)

- [RAID 5 vs RAID 6](https://www.youtube.com/watch?v=UuUgfCvt9-Q)

- [10.5 Git Internals - The Refspec](https://git-scm.com/book/en/v2/Git-Internals-The-Refspec?utm_source=chatgpt.com)

- [Creating a Kubernetes Cluster on Bare-metal](https://docs.kubermatic.com/kubeone/v1.10/tutorials/creating-clusters-baremetal/)
