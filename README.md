# KubeAid Bootstrap Script

The `KubeAid Bootstrap Script` is used to bootstrap Kubernetes clusters using Cluster API and [KubeAid](https://github.com/Obmondo/KubeAid).

> Currently it only supports bootstrapping self-managed clusters in AWS.

## Official Guides

- [Bootstrapping a self-managed cluster in AWS](https://github.com/Obmondo/KubeAid/blob/master/docs/aws/capi/cluster.md)

## Developer Guide (AWS edition)

> Make sure, you've Docker installed and running in your system.

Run `make build-image-dev` to build the KubeAid Bootstrap Script container image (development version).

Then run `make run-container-dev` to run the container.

If you're running MacOS, then in your host machine, make sure you have mapped `host.docker.internal` to **127.0.0.1** in your **/etc/hosts**.

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

## TODOs

- [ ] Check Git URL if SSH agent is used.
- [ ] Validation for sshagentauth (should not accept https url).
- [x] `--debug` flag to print command execution outputs.
- [x] Support adding multiple SSH keys via config file.
- [ ] Support using HTTPS for ArgoCD apps.
- [x] Support enabling `Audit Logging`.
- [x] Switch to IAM Role from (temporary) credentials after cluster bootstrap.
- [x] ETCD metrics enabled.
- [x] Support scale to / from zero for the node-groups.
  > Currently, I have added extra ClusterRole and ClusterRoleBinding in the KubeAid [cluster-autoscaler Helm chart](https://github.com/Obmondo/kubeaid/tree/master/argocd-helm-charts/cluster-autoscaler) to support this feature.
  > But I have also opened an issue in the kubernetes-sigs/autoscaler repository regarding this : [Allow adding extra rules to the Role / ClusterRole template of the Cluster AutoScaler Helm chart](https://github.com/kubernetes/autoscaler/issues/7680).
- [ ] In case of AWS, pick up AWS credentials from `~/.aws/credentials` (if present).
- [ ] `recover cluster` command

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
