# DCM Helm Chart

## Prerequisites

- Kubernetes 1.24+ or OpenShift 4.12+
- Helm 3.x
- A default StorageClass configured in the cluster (for PostgreSQL and NATS persistent volumes)

## Quick Start

Install all the components with a kubernetes provider using default namespace.

### OpenShift

```bash
helm install dcm deploy/helm/dcm \
  --set k8sContainerServiceProvider.enabled=true \
  --set k8sContainerServiceProvider.namespace=default
```

OpenShift Routes are enabled by default for control-plane and DCM UI.

### Kubernetes

```bash
helm install dcm deploy/helm/dcm \
  --set controlPlane.route.enabled=false \
  --set dcmUi.route.enabled=false \
  --set k8sContainerServiceProvider.enabled=true \
  --set k8sContainerServiceProvider.namespace=default
```

Access via port-forward:

```bash
kubectl port-forward svc/dcm-control-plane 8080:8080
kubectl port-forward svc/dcm-dcm-ui 7007:7007
```

Then open:
- Control-plane API: http://localhost:8080
- DCM UI: http://localhost:7007

## Enabling Service Providers

### KubeVirt Service Provider

Manages virtual machines via KubeVirt.

```bash
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set kubevirtServiceProvider.enabled=true \
  --set kubevirtServiceProvider.namespace=default
```

### ACM Cluster Service Provider

Manages clusters via Red Hat Advanced Cluster Management.

**Pull secret** (required): Provide a base64-encoded `.dockerconfigjson` via one of:

- `pullSecret`: inline value (stored in a chart-managed Secret via `stringData`)
- `pullSecretRef`: name of a pre-existing Secret **in the release namespace** with a
  `stringData` key `pull-secret` whose value is the base64-encoded `.dockerconfigjson` string
  (`secretKeyRef` is same-namespace only)

> If both are set, `pullSecret` takes precedence and `pullSecretRef` is ignored.

```bash
# Encode your pull secret
PULL_SECRET=$(oc get secret pull-secret -n openshift-config -o jsonpath='{.data.\.dockerconfigjson}')
```

**Cluster access**: When both `kubeconfig` and `kubeconfigRef` are omitted, the chart creates
a ServiceAccount with RBAC for HyperShift, Hive, KubeVirt, Agent and core Secret APIs (in-cluster auth on the
hub). To use an external kubeconfig instead, provide it via one of:

- `kubeconfig`: raw kubeconfig content (stored in a chart-managed Secret)
- `kubeconfigRef`: name of a pre-existing Secret **in the release namespace** with key `kubeconfig`

```bash
# In-cluster mode (SA + RBAC created by chart):
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set acmClusterServiceProvider.enabled=true \
  --set acmClusterServiceProvider.namespace=default \
  --set acmClusterServiceProvider.baseDomain=example.com \
  --set acmClusterServiceProvider.pullSecret="$PULL_SECRET"

# External kubeconfig mode (inline):
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set acmClusterServiceProvider.enabled=true \
  --set acmClusterServiceProvider.namespace=default \
  --set acmClusterServiceProvider.baseDomain=example.com \
  --set acmClusterServiceProvider.pullSecret="$PULL_SECRET" \
  --set-file acmClusterServiceProvider.kubeconfig=/path/to/kubeconfig

# External kubeconfig mode (pre-existing Secret):
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set acmClusterServiceProvider.enabled=true \
  --set acmClusterServiceProvider.namespace=default \
  --set acmClusterServiceProvider.baseDomain=example.com \
  --set acmClusterServiceProvider.pullSecret="$PULL_SECRET" \
  --set acmClusterServiceProvider.kubeconfigRef=my-kubeconfig-secret
```

### Three-Tier Demo Service Provider

A demo provider for a three-tier application. Requires the Kubernetes Container Service Provider to also be enabled.

```bash
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set k8sContainerServiceProvider.enabled=true \
  --set threeTierDemoServiceProvider.enabled=true
```

## Uninstall

```bash
helm uninstall dcm
```

Note: PersistentVolumeClaims for PostgreSQL and NATS are not deleted automatically. To remove them:

```bash
kubectl delete pvc -l app.kubernetes.io/instance=dcm
```
