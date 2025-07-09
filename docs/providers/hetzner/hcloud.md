## Hetzner cloud

### Generate token and ssh keys
- Generate an [HCloud API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token)

- Generate an [SSH key pair](https://community.hetzner.com/tutorials/add-ssh-key-to-your-hetzner-cloud)

- Generate the [GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token).

### Setup

1. **Configure Your Environment**:
   - Setup the .env file
   ```raw
   # cat .env
   CLOUD_PROVIDER=hetzner
   FLAVOR=hcloud
   ```

2. **Generate the config**:
   - Run the compose to generate the config, it will drop the file in **/outputs/config**
   ```bash
   docker compose run bootstrap-generate
   ```

3. **Add the user and ssh key**:
   - Edit general.yaml
   ```yaml
   # Any additional users you want to be setup for each Kubernetes node.
   additionalUsers:
    - name: your-username
      sshPublicKey: xxxxxxxxxx
   ```

4. **Add the git username and token**:
   - Edit secret.yaml
   ```yaml
   git:
     username: xxxxxxxxxx
     password: xxxxxxxxxx
   ```

5. **Bootstrap the cluster**:
   - Setup the Hetzner cloud k8s cluster
   ```sh
   docker compose run bootstrap-cluster
   ```

6. **Access Your Cluster**:
   - Once the setup is complete, you can access your Kubernetes cluster using `kubectl`:
   ```bash
   export KUBECONFIG=./outputs/kubeconfigs/clusters/main.yaml
   kubectl get nodes
   ```

## Reference

https://syself.com/docs/caph/topics/managing-ssh-keys#in-hetzner-cloud
