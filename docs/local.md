## Local KubeAid Cluster

### Generate token and ssh keys

- Generate the [GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token).

### Setup

1. **Download the compose file**:
   - Get the compose file
   ```bash
   wget https://raw.githubusercontent.com/Obmondo/kubeaid-bootstrap-script/refs/heads/main/docker-compose.yaml
   ```

2. **Configure Your Environment**:
   - Setup the .env file
   ```raw
   # vim .env
   CLOUD_PROVIDER=local
   CLUSTER_NAME=kubeaid-demo
   ```

3. **Generate the config**:
   - Run the compose to generate the config, it will drop the file in **/outputs/config**
   ```bash
   docker compose run bootstrap-generate
   ```

4. **Fix the config based on your requirements**:
   - kubeaid-config git repo in general.yaml
   ```yaml
   forkURLs:
     kubeaidConfig: https://github.com/xxxxxxxx/kubeaid-config.git
   ```

5. **Deploy the local Cluster**:
   - Run the docker compose:
   ```bash
   docker compose run bootstrap-cluster
   ```

6. **Access Your Cluster**:
   - Once the setup is complete, you can access your Kubernetes cluster using `kubectl`:
   ```bash
   export KUBECONFIG=./outputs/kubeconfigs/main.yaml
   kubectl get nodes
   ```

7. **Access Keycloak, ArgoCD, and Monitoring**:
   - Follow the instructions in the [KubeAid](https://github.com/Obmondo/kubeaid) to access and configure Keycloak, ArgoCD, and Kube-Prometheus.
   - Example: ArgoCD
   ```bash
   kubectl port-forward svc/argocd-server --namespace argo-cd 8080:443
   ```

8. **Destroy**
   - When you are done playing
   ```bash
   k3d cluster delete kubeaid-demo
   ```
