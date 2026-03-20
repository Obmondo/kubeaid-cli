# E2E Test Plan for kubeaid-cli

## Overview

E2E tests for kubeaid-cli across four provider targets: Local (K3D), Hetzner, AWS, and Azure. Tests use Go's `testing` package with `//go:build e2e` build tag.

## Directory Structure

```
e2e/
  compose/                         # (existing) Gitea docker-compose
  testdata/
    configs/
      local/
        general.yaml
        secrets.yaml
      hetzner/
        bare-metal/
        hcloud/
        hybrid/
      aws/
      azure/
    payloads/
      hetzner/
        robot/                     # Sample Robot API response JSONs
        hcloud/                    # Sample HCloud API response JSONs
  helpers/
    helpers.go                     # Shared test utilities
    mock_hetzner_robot.go          # httptest-based Hetzner Robot API mock
    mock_hcloud.go                 # httptest-based HCloud API mock
    disk_sim.go                    # Loop device + dd disk simulation
    docker_helpers.go              # Container/network management
  step1_local_test.go
  step2_hetzner_test.go
  step3_aws_test.go
  step4_azure_test.go
```

## Required Refactoring

1. **Hetzner client injection**: Modify `NewHetznerCloudProvider()` to accept options for base URL and HTTP client overrides (for Robot and HCloud mocking)
2. **Interactive approval bypass**: Add `KUBEAID_SKIP_APPROVAL=true` env var check in `GetApproval()`
3. **Globals are already settable** (existing tests do this)

---

## Step 1: Local Dev (K3D)

### What to Test

| Test | Description |
|------|-------------|
| Management cluster creation | Call `k3d.CreateK3DCluster()`, verify kubeconfig is valid |
| Full local bootstrap | Run `BootstrapCluster()` with `CloudProviderLocal` config |
| ArgoCD health | Verify argocd-server, repo-server, application-controller pods are Running |
| Sealed Secrets | Verify sealed-secrets-controller pod is Running |
| Cert-manager | Verify cert-manager pods are Running |
| Parameter variation | Test with different K8s versions, SkipMonitoringSetup true/false |
| Idempotency | Call BootstrapCluster twice, verify no errors |

### Chaos Engineering Parameters

- Vary `Cluster.K8sVersion` (e.g., v1.33.x, v1.34.x)
- Toggle `SkipMonitoringSetup`
- Toggle `SkipPRWorkflow`
- Kill ArgoCD pods mid-sync, verify recovery
- Delete sealed-secrets controller, verify re-creation

### Prerequisites

- Docker running
- Gitea compose started (existing `e2e/compose/docker-compose.yaml`)

---

## Step 2: Hetzner

### Mock Strategy

#### Hetzner Robot API Mock (`httptest.Server`)

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/server/{id}` | GET | Get server IP and details |
| `/key` | GET/POST | SSH key pair management |
| `/vswitch` | GET/POST | VSwitch creation |
| `/vswitch/{id}/server` | POST | Attach server to VSwitch |
| `/failover/{ip}` | GET/POST | Failover IP management |

The mock tracks state (which servers are attached to which vswitches) and validates operation sequences.

#### HCloud API Mock (`httptest.Server`)

| Resource | Purpose |
|----------|---------|
| Network.Get/Create/AddSubnet | Network creation |
| Server.List/AttachToNetwork | Server management |
| ServerType.GetByName | VM specs lookup |

Use `hcloud.WithEndpoint()` to point at mock server.

#### Disk Simulation (for bare-metal storage planning)

**Approach**: Privileged Docker containers with loop devices.

```sh
# Inside container
dd if=/dev/zero of=/disk0 bs=1G count=0 seek=100
losetup /dev/loop0 /disk0
```

- Containers expose SSH, Robot API mock returns container IP as server IP
- `getServerDisks()` SSHes into containers, `lsblk` shows loop devices
- No real disk wipe — mock Robot API acknowledges installImage requests
- Verifies correct API calls were made (server IDs, image paths)

**Why Docker over LXC**: Privileged Docker containers are simpler, more portable, and available on CI runners without extra setup.

### What to Test

| Test | Description |
|------|-------------|
| Storage plan generation | Feed mock servers with known disk configs, verify allocation |
| Network creation | Call CreateNetwork() against mock HCloud API |
| VSwitch management | Create VSwitch, attach servers via mock Robot API |
| SSH key validation | Verify key creation/skip logic against mock |
| Failover IP routing | Verify failover IP call via mock |
| HCloud full flow | Bootstrap with mock APIs up to ClusterAPI sync point |
| Hybrid partial flow | Test network + vswitch + server attachment |

### The Disk Wipe Challenge

The Hetzner Robot setup wipes disks via installImage. In tests:
- Mock Robot API acknowledges the request without wiping
- Verify correct API calls (server IDs, image paths)
- Loop devices in Docker containers provide realistic `lsblk` output
- Storage plan verification uses synthetic disk data

---

## Step 3: AWS (TODO)

### Mock Strategy (future)

- Use `httptest.Server` or `localstack` via testcontainers
- AWS SDK supports custom HTTP clients and endpoint resolvers
- Mock IAM CloudFormation stack creation
- Mock EC2 instance type lookups
- Mock S3 bucket operations for disaster recovery

### Placeholder

```go
func TestAWSBootstrap(t *testing.T) {
    t.Skip("AWS e2e tests not yet implemented")
}
```

---

## Step 4: Azure (TODO)

### Mock Strategy (future)

- Azure SDK supports custom HTTP transports via `policy.ClientOptions.Transport`
- Mock identity/credential operations
- Mock storage account operations
- Mock VM size lookups

### Placeholder

```go
func TestAzureBootstrap(t *testing.T) {
    t.Skip("Azure e2e tests not yet implemented")
}
```

---

## CI Pipeline Integration

### Makefile Targets

```makefile
.PHONY: test-unit
test-unit:
	@go test -v -count=1 ./...

.PHONY: test-e2e-local
test-e2e-local:
	@docker compose -f e2e/compose/docker-compose.yaml up -d --wait
	@go test -v -count=1 -tags=e2e -timeout=30m ./e2e/ -run TestLocal
	@docker compose -f e2e/compose/docker-compose.yaml down

.PHONY: test-e2e-hetzner
test-e2e-hetzner:
	@go test -v -count=1 -tags=e2e -timeout=30m ./e2e/ -run TestHetzner

.PHONY: test-e2e
test-e2e: test-e2e-local test-e2e-hetzner
```

### Gitea Actions Workflow

Add to `.gitea/workflows/pr.yaml`:

```yaml
e2e-tests-local:
  runs-on: ubuntu-22.04-htzhel1-ax42-a
  steps:
    - uses: checkout@v4
    - uses: setup-go@v5
      with:
        go-version-file: go.mod
    - name: Start Gitea
      run: docker compose -f e2e/compose/docker-compose.yaml up -d --wait
    - name: Run local e2e tests
      run: go test -v -count=1 -tags=e2e -timeout=30m ./e2e/ -run TestLocal
    - name: Cleanup
      if: always()
      run: docker compose -f e2e/compose/docker-compose.yaml down
```

---

## Implementation Phases

| Phase | Scope | Timeline |
|-------|-------|----------|
| 1. Foundation | Directory structure, build tags, Makefile targets, helpers, Hetzner refactor | Week 1-2 |
| 2. Local/K3D | step1_local_test.go, Gitea integration, health checks, chaos tests | Week 2-3 |
| 3. Hetzner | Robot/HCloud mocks, disk simulation, step2_hetzner_test.go | Week 3-5 |
| 4. Placeholders | AWS/Azure placeholder files, mock strategy docs | Week 5 |

---

## Key Challenges

| Challenge | Mitigation |
|-----------|------------|
| Package-level globals | Use `t.Cleanup()` to restore. Run sequentially. |
| `assert.Assert()` calls `os.Exit(1)` | Acceptable for e2e. Long-term: refactor to return errors. |
| Hetzner disk wipe | Mock Robot API, loop devices for `lsblk` output. |
| Interactive approval | Skip via env var `KUBEAID_SKIP_APPROVAL=true`. |
| Long execution times | 30min timeout, separate CI jobs, cache K3D cluster in `TestMain`. |
| ClusterAPI reconciliation | Test up to sync trigger, verify ArgoCD app calls. |
