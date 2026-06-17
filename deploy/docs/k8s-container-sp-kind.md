# K8s Container Service Provider with Kind

When Kind runs with Podman, its API server lives on a separate
Podman network. The compose services can't reach it by default.

## Setup (one-time, until containers are recreated)

### 1. Create the Kind cluster

```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster
```

This creates a container named `kind-control-plane`.
If you use `--name <name>`, the container will be `<name>-control-plane`.

### 2. Start the compose services

This must be done **before** step 3 so the compose network exists.

```bash
podman compose -f deploy/compose.yaml --profile k8s-container up -d
```

### 3. Connect Kind to the compose network

Connect the Kind control-plane container to the compose network with
an alias that matches a SAN in Kind's API server certificate.

To list valid SANs:

```bash
podman exec kind-control-plane \
  openssl x509 -in /etc/kubernetes/pki/apiserver.crt -noout -text \
  | grep -A1 "Subject Alternative Name"
```

Use `kubernetes` as the alias (short, always present in the SAN list):

```bash
podman network connect \
  --alias kubernetes \
  deploy_default \
  kind-control-plane
```

> **Note:** `make compose-up` sets `COMPOSE_PROJECT_NAME=control-plane`, so the
> network is `deploy_default`. Verify with `podman network ls`.
> `make compose-down` disconnects Kind and removes the network automatically.

### 4. Generate a kubeconfig that uses the alias

```bash
kubectl config view --minify --flatten --context kind-kind \
  | sed -E 's|https://[^:]+:[0-9]+|https://kubernetes:6443|' \
  > kubeconfig.yaml
```

Kind maps the API server to a random host port (e.g. `44615`), but
container-to-container traffic uses port `6443` directly.

### 5. Point the SP to the generated kubeconfig

The compose file mounts `${K8S_CONTAINER_SP_KUBECONFIG:-~/.kube/config}` into the
SP container. Set the variable to the generated file and restart:

```bash
export K8S_CONTAINER_SP_KUBECONFIG="$(pwd)/kubeconfig.yaml"
podman compose -f deploy/compose.yaml --profile k8s-container up -d
```

### External service type

The SP requires `SP_K8S_EXTERNAL_SVC_TYPE` to determine the Kubernetes Service
type for ports with `external` visibility. Valid values:

| Value | Use case |
|---|---|
| `NodePort` | Default. Works out of the box with Kind and bare-metal clusters. |
| `LoadBalancer` | Cloud environments with a load-balancer controller (e.g., AWS, GCP) or clusters running MetalLB. |

The compose file defaults to `NodePort`. Override it with:

```bash
export K8S_CONTAINER_SP_EXTERNAL_SVC_TYPE=LoadBalancer
```

## Why this is needed

| Problem | Cause |
|---|---|
| SP container can't reach Kind's IP | That IP belongs to Kind's network; compose services are on `deploy_default` |
| SP connects to `127.0.0.1:<random-port>` | Kind's default kubeconfig uses the host-side port mapping, unreachable from other containers |
| TLS error using arbitrary hostname | The API server certificate only includes specific SANs |

Connecting Kind to the compose network with a certificate-valid alias
and generating a kubeconfig that targets it solves all three.
