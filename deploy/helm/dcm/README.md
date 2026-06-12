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

```bash
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set acmClusterServiceProvider.enabled=true \
  --set acmClusterServiceProvider.namespace=default \
  --set acmClusterServiceProvider.baseDomain=example.com
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
