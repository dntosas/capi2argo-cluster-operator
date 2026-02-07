# Capi2Argo Cluster Operator

[![CI](https://github.com/dntosas/capi2argo-cluster-operator/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/dntosas/capi2argo-cluster-operator/actions/workflows/ci.yml) | [![Go Report](https://goreportcard.com/badge/github.com/dntosas/capi2argo-cluster-operator)](https://goreportcard.com/badge/github.com/dntosas/capi2argo-cluster-operator) | [![Go Release](https://github.com/dntosas/capi2argo-cluster-operator/actions/workflows/go-release.yml/badge.svg)](https://github.com/dntosas/capi2argo-cluster-operator/actions/workflows/go-release.yml) | [![Helm Chart Release](https://github.com/dntosas/capi2argo-cluster-operator/actions/workflows/helm-release.yml/badge.svg)](https://github.com/dntosas/capi2argo-cluster-operator/actions/workflows/helm-release.yml) | [![codecov](https://codecov.io/gh/dntosas/capi2argo-cluster-operator/branch/main/graph/badge.svg?token=5GDS0GGTY3)](https://codecov.io/gh/dntosas/capi2argo-cluster-operator)

**Capi-2-Argo Cluster Operator (CACO)** converts [ClusterAPI](https://cluster-api.sigs.k8s.io/) cluster credentials into [ArgoCD](https://argo-cd.readthedocs.io/en/stable/) cluster definitions and keeps them synchronized. It bridges the automation gap for teams that use ClusterAPI to provision Kubernetes clusters and ArgoCD to manage workloads on them.

## The Problem

[ClusterAPI](https://cluster-api.sigs.k8s.io/) provides declarative APIs for provisioning, upgrading, and operating multiple Kubernetes clusters. [ArgoCD](https://argo-cd.readthedocs.io/en/stable/) is a GitOps continuous delivery tool that deploys applications to target Kubernetes clusters.

A typical pipeline looks like this:

![flow-without-capi2argo](docs/flow-without-operator.png)

1. Git holds Kubernetes cluster definitions as CRDs
2. ArgoCD watches these resources from Git
3. ArgoCD deploys definitions on a Management Cluster
4. ClusterAPI reconciles the definitions
5. ClusterAPI provisions clusters on the cloud provider
6. ClusterAPI stores provisioned cluster credentials as Kubernetes Secrets
7. :x: **ArgoCD has no way to discover and authenticate to the new clusters**

## The Solution

CACO watches for CAPI-managed kubeconfig secrets, converts them into ArgoCD-compatible cluster secrets, and keeps them in sync. This closes the loop:

1. CACO detects CAPI cluster secrets
2. CACO converts them to ArgoCD cluster definitions
3. CACO creates/updates them in the ArgoCD namespace
4. ArgoCD discovers the new clusters
5. :heavy_check_mark: **ArgoCD deploys workloads to CAPI-provisioned clusters**

![flow-with-capi2argo](docs/flow-with-operator.png)

### Secret Transformation

CACO transforms a CAPI kubeconfig secret:

```yaml
kind: Secret
apiVersion: v1
type: cluster.x-k8s.io/secret
metadata:
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
  name: my-cluster-kubeconfig
data:
  value: << base64-encoded kubeconfig >>
```

Into an ArgoCD cluster secret:

```yaml
kind: Secret
apiVersion: v1
type: Opaque
metadata:
  labels:
    argocd.argoproj.io/secret-type: cluster
    capi-to-argocd/owned: "true"
  name: cluster-my-cluster
  namespace: argocd
stringData:
  name: my-cluster
  server: https://my-cluster.example.com:6443
  config: |
    {
      "tlsClientConfig": {
        "caData": "<base64-ca>",
        "certData": "<base64-cert>",
        "keyData": "<base64-key>"
      }
    }
```

## Installation

### Helm (Recommended)

```console
helm repo add capi2argo https://dntosas.github.io/capi2argo-cluster-operator/
helm repo update
helm upgrade -i capi2argo capi2argo/capi2argo-cluster-operator
```

See the [chart values](./charts/capi2argo-cluster-operator/README.md) for all available configuration options.

## Configuration

CACO is configured through environment variables (set via Helm values):

| Environment Variable | Helm Value | Default | Description |
|---|---|---|---|
| `ARGOCD_NAMESPACE` | `argoCDNamespace` | `argocd` | Namespace where ArgoCD cluster secrets are created |
| `ALLOWED_NAMESPACES` | `allowedNamespaces` | `""` (all) | Comma-separated list of namespaces to watch. Empty means all namespaces |
| `ENABLE_GARBAGE_COLLECTION` | `garbageCollectionEnabled` | `false` | Delete ArgoCD secrets when the corresponding CAPI secret is deleted |
| `ENABLE_NAMESPACED_NAMES` | `namespacedNamesEnabled` | `false` | Prepend cluster namespace to ArgoCD secret names to avoid collisions |
| `ENABLE_AUTO_LABEL_COPY` | *(via `extraEnvVars`)* | `false` | Automatically copy all non-system labels from CAPI Cluster to ArgoCD secret |

### CLI Flags

| Flag | Default | Description |
|---|---|---|
| `--sync-duration` | `45s` | How often to re-sync cluster secrets |
| `--metrics-bind-address` | `:8080` | Address for the Prometheus metrics endpoint |
| `--health-probe-bind-address` | `:8081` | Address for health/readiness probes |
| `--leader-elect` | `false` | Enable leader election for HA deployments |
| `--debug` | `false` | Enable debug logging |
| `--dry-run` | `false` | Run without making changes |

## Features

### Namespace Filtering

By default CACO watches all namespaces for CAPI secrets. To limit it to specific namespaces, set a comma-separated list:

```yaml
# values.yaml
allowedNamespaces: "team-a,team-b,production"
```

Or via environment variable:

```bash
ALLOWED_NAMESPACES=team-a,team-b,production
```

Secrets in namespaces not in the list are ignored entirely (they don't even trigger reconciliation).

### Garbage Collection

When enabled, CACO deletes the corresponding ArgoCD secret when a CAPI kubeconfig secret is deleted:

```yaml
# values.yaml
garbageCollectionEnabled: true
```

### Ignoring Clusters

To exclude a specific cluster from being synced to ArgoCD, add the ignore label to the `Cluster` resource:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cluster
  labels:
    ignore-cluster.capi-to-argocd: ""
```

### Take-Along Labels

CACO can copy labels from a `Cluster` resource to the generated ArgoCD secret. This is useful for ArgoCD [ApplicationSet generators](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/) that select clusters by label.

Add a label with the format `take-along-label.capi-to-argocd.<label-key>: ""`:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cluster
  namespace: default
  labels:
    env: production
    team: platform
    take-along-label.capi-to-argocd.env: ""
    take-along-label.capi-to-argocd.team: ""
```

The resulting ArgoCD secret will include:

```yaml
metadata:
  labels:
    env: production
    team: platform
    taken-from-cluster-label.capi-to-argocd.env: ""
    taken-from-cluster-label.capi-to-argocd.team: ""
```

### Auto Label Copy

As an alternative to take-along labels, enable automatic copying of **all** non-system labels:

```bash
ENABLE_AUTO_LABEL_COPY=true
```

This copies every label from the Cluster resource to the ArgoCD secret, except:
- `kubernetes.io/*` (system labels)
- `cluster.x-k8s.io/*` (CAPI internal labels)
- `capi-to-argocd/*` (controller internal labels)

### Namespaced Names

When managing clusters across multiple namespaces, name collisions can occur (e.g., two namespaces both have a cluster named `prod`). Enable namespaced names to prepend the namespace:

```yaml
# values.yaml
namespacedNamesEnabled: true
```

This changes the ArgoCD secret name from `cluster-prod` to `cluster-<namespace>-prod`.

### Rancher Support

CACO supports Rancher-managed clusters that use `Opaque` secret types instead of the standard CAPI type. Opaque secrets are accepted if they carry the `cluster.x-k8s.io/cluster-name` label.

### Prometheus Metrics

CACO exposes custom metrics on the `/metrics` endpoint:

| Metric | Type | Description |
|---|---|---|
| `caco_argocd_secrets_created_total` | Counter | Total ArgoCD cluster secrets created |
| `caco_argocd_secrets_updated_total` | Counter | Total ArgoCD cluster secrets updated |
| `caco_argocd_secrets_deleted_total` | Counter | Total ArgoCD cluster secrets deleted |

Enable the ServiceMonitor in the Helm chart:

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
```

## Use Cases

1. **DRY Production Pipelines** - Everything as testable, version-controlled code
2. **Automated Credential Management** - No manual steps, UI clicks, or cron scripts
3. **End-to-End Infrastructure Testing** - Bundle cluster provisioning and workload deployment
4. **Dynamic Environments** - Automatically register new clusters with ArgoCD as they are provisioned

## Development

### Prerequisites

- Go 1.24+
- A running Kubernetes cluster (or [kind](https://kind.sigs.k8s.io/) for local development)
- [envtest](https://book.kubebuilder.io/reference/envtest.html) binaries (installed automatically via `make envtest`)

### Common Commands

```console
make fmt          # Format code
make vet          # Run go vet
make lint         # Run golangci-lint
make test         # Run tests (unit + integration via envtest)
make ci           # Run fmt + vet + lint + test
make build        # Build the binary (linux/amd64)
make build-darwin # Build the binary (darwin/arm64)
make run          # Run the controller locally against current kubeconfig
make modsync      # Run go mod tidy + vendor
```

### Local Development with Helm

```console
make docker-build-dev   # Build dev Docker image
make helm-deploy-dev    # Deploy to current cluster via Helm
```

## Contributing

Contributions are welcome! Feel free to open issues or pull requests.
