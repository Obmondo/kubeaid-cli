## Hetzner cloud

## Generate token and ssh keys
- Generate an [HCloud API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token)

- Generate an [SSH key pair](https://community.hetzner.com/tutorials/add-ssh-key-to-your-hetzner-cloud)

- Generate the [GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token).

## Bootstrap the cluster

* Add the user and ssh key in the general.yaml.

```yaml
# Any additional users you want to be setup for each Kubernetes node.
additionalUsers:
 - name: your-username
   sshPublicKey: xxxxxxxxxx
```

* Add the git username and token in the secret.yaml

```yaml
git:
  username: xxxxxxxxxx
  password: xxxxxxxxxx
```

* Bootstrap the cluster

```sh
docker compose run bootstrap-cluster
```

## Reference

https://syself.com/docs/caph/topics/managing-ssh-keys#in-hetzner-cloud
