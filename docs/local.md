## Local KubeAid Cluster

### Generate token and ssh keys

- Generate the [GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token).

### Setup

1. **Setup your kubeaid-config git repo [here](https://github.com/Obmondo/kubeaid-bootstrap-script/tree/main?tab=readme-ov-file#quick-setup)**

2. **Deploy the local Cluster**:
   - Run the docker compose:
   ```bash
   docker compose run bootstrap-cluster
   ```

3. **Access Your Cluster**:
   - Once the setup is complete, you can access your Kubernetes cluster using `kubectl`:
   ```bash
   export KUBECONFIG=./outputs/kubeconfigs/main.yaml
   kubectl get nodes
   ```

4. **Access Keycloak, ArgoCD, and Monitoring**:
   - Follow the instructions in the [KubeAid](https://github.com/Obmondo/kubeaid) to access and configure Keycloak, ArgoCD, and Kube-Prometheus.
   - Example: ArgoCD
   ```bash
   kubectl port-forward svc/argocd-server --namespace argo-cd 8080:443
   ```

5. **Destroy**
   - When you are done playing
   ```bash
   k3d cluster delete kubeaid-demo
   ```
