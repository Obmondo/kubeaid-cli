# KubeAid Bootstrap Script

The `KubeAid Bootstrap Script` is used to bootstrap Kubernetes clusters using Cluster API and [KubeAid](https://github.com/Obmondo/KubeAid).

> Currently it only supports bootstrapping self-managed clusters in AWS.

## Official Guides

- [Bootstrapping a self-managed cluster in AWS](https://github.com/Obmondo/KubeAid/blob/master/docs/aws/capi/cluster.md)

## Developer Guide (AWS)

> Make sure, you've Docker installed in your system.

Run `make build-image-dev` to build the KubeAid Bootstrap Script container image (development version).

Then run `make run-container-dev` to run the container.

In a separate terminal window, use `make exec-container-dev` to execute into the container.

Once you're inside the container, use `make generate-sample-config-aws-dev` to generate a sample config file at [./outputs/kubeaid-bootstrap-script.config.yaml](./outputs/kubeaid-bootstrap-script.config.yaml), targetting the AWS cloud provider. Adjust the config file according to your needs.

Export your AWS credentials as environment variables and then run `make bootstrap-cluster-dev` to bootstrap the cluster!

### Debugging

- Check ClusterAPI related pod logs.

- SSH into the control-plane node. In case of AWS, you can view cloud-init logs stored at `/var/log/cloud-init-output.log`.

## TODOs

- [ ] Check Git URL if SSH agent is used.
- [ ] Validation for sshagentauth (should not accept https url).
- [x] `--debug` flag to print command execution outputs.
- [ ] Support adding admin SSH keys via config file.
- [ ] Support using HTTPS for ArgoCD apps.
- [ ] Use ArgoCD sync waves so that we don't need to explicitly sync the Infrastructure Provider component first.
- [x] Support enabling `Audit Logging`.
- [x] Switch to IAM Role from (temporary) credentials after cluster bootstrap.
- [x] ETCD metrics enabled.
- [x] Support scale to / from zero for the node-groups.
  > Currently, I have added extra ClusterRole and ClusterRoleBinding in the KubeAid [cluster-autoscaler Helm chart](https://github.com/Obmondo/kubeaid/tree/master/argocd-helm-charts/cluster-autoscaler) to support this feature.
  > But I have also opened an issue in the kubernetes-sigs/autoscaler repository regarding this : [Allow adding extra rules to the Role / ClusterRole template of the Cluster AutoScaler Helm chart](https://github.com/kubernetes/autoscaler/issues/7680).
- [ ] `recover cluster` command

## REFERENCES

- [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/#comparison-with-client-side-apply)

- [The definitive guide to building Golang based CLI](https://www.youtube.com/watch?v=SSRIn5DAmyw)

- [AWS S3 Sync Command – Guide with Examples](https://spacelift.io/blog/aws-s3-sync)

- How KubeAid backs up Sealed Secrets using a CRONJob : https://github.com/Obmondo/kubeaid/blob/master/argocd-helm-charts/sealed-secrets/templates/configmap.yaml

- [Key Management](https://playbook.stakater.com/content/workshop/sealed-secrets/management.html)

- [Secret Rotation](https://github.com/bitnami-labs/sealed-secrets?tab=readme-ov-file#secret-rotation)

- [Kubernetes Backups, Upgrades, Migrations - with Velero](https://youtu.be/zybLTQER0yY?si=qOZcizBqPOeouJ7y)

- [Failover](https://docs.hetzner.com/robot/dedicated-server/ip/failover/)

- [Auditing](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/)

- [Kube API server args](https://kubernetes.io/docs/reference/command-line-tools-reference/kube-apiserver/)

- [Using IAM roles in management cluster instead of AWS credentials](https://cluster-api-aws.sigs.k8s.io/topics/using-iam-roles-in-mgmt-cluster)

- [KubeadmControlPlane CRD](https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml)

- [How can you call a helm 'helper' template from a subchart with the correct context?](https://stackoverflow.com/questions/47791971/how-can-you-call-a-helm-helper-template-from-a-subchart-with-the-correct-conte)
