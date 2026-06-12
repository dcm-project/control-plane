# Three-Tier Demo App Service Provider with Kind

The Three-Tier Demo App Service Provider (SP) is a DCM plugin that provisions a Pet Clinic application
into a Kubernetes cluster. It requires the k8s-container-service-provider to be configured and running.

## Setup (one-time, until containers are recreated)

### CLI or curl

In the Compose setup from this repo, the control-plane API is **http://localhost:8080** by default. The
sections below give **`dcm`** ([CLI repo](https://github.com/dcm-project/cli)) and **`curl`** for the
same operations. They are equivalent, so use whichever you prefer.
Build or install **`dcm`** per the [CLI README](https://github.com/dcm-project/cli/blob/main/README.md).
Set the control-plane API URL in **`~/.dcm/config.yaml`** (`control-plane-url`) or via
**`DCM_CONTROL_PLANE_URL`** if the API is not on localhost:8080 (default). Legacy aliases
**`api-gateway-url`** and **`DCM_API_GATEWAY_URL`** still work.

### Prerequisites

Before starting the three-tier SP, you must complete the k8s-container-service-provider setup:

1. Follow steps 1–5 in [k8s-container-sp-kind.md](k8s-container-sp-kind.md) to set up:
   - A Kind cluster connected to the Compose network
   - A kubeconfig configured to use the `kubernetes` alias
   - The k8s-container-service-provider running and healthy

Verify the setup:

```bash
dcm sp provider list
```

Or:

```bash
curl -s http://localhost:8080/api/v1alpha1/providers | jq '.providers[] | {name, health_status}'
```

> **Note:** The response should include `k8s-container-provider` in the list of available providers.

### 1. Start the three-tier SP

Using the Makefile (recommended):

```bash
export K8S_CONTAINER_SP_KUBECONFIG="$(pwd)/kubeconfig.yaml"
make compose-up-with-providers PROFILES=three-tier
```

Or the same compose invocation without Make:

```bash
export K8S_CONTAINER_SP_KUBECONFIG="$(pwd)/kubeconfig.yaml"
podman compose --profile three-tier up -d
```

> **Note:** The three-tier profile automatically includes the k8s-container-service-provider 
> as a dependency. Both services will start together.

Verify it is running:

```bash
podman ps --format "table {{.Names}}\t{{.Status}}" | grep -E 'three-tier|k8s-container'
```

Check the SP is registered with DCM:

```bash
dcm sp provider list
```

Or:

```bash
curl -s http://localhost:8080/api/v1alpha1/providers | jq '.providers[] | select(.name | contains("three-tier"))'
```

Note the provider’s `name` (by default `three-tier-provider` unless you changed `THREE_TIER_SP_NAME` in Compose).

### 2. Provision the Pet Clinic application

Register a placement policy once, then create a catalog item instance via the
control-plane API (**`dcm`** or **`curl`**). The **First time only (policy)** note sits next to the commands in each subsection.

#### With DCM CLI

Create a folder and both YAML files there (copy and paste the whole block):

```bash
mkdir -p /tmp/dcm-petclinic

cat > /tmp/dcm-petclinic/three-tier-placement.yaml <<'EOF'
display_name: Three-tier placement
policy_type: GLOBAL
enabled: true
priority: 100
rego_code: |
  package policies.three_tier_default

  main := {
    "rejected": false,
    "selected_provider": "three-tier-provider"
  } if {
    input.spec.service_type == "three-tier-app-demo"
  }
EOF

cat > /tmp/dcm-petclinic/my-petclinic.yaml <<'EOF'
api_version: v1alpha1
display_name: my-petclinic
spec:
  catalog_item_id: pet-clinic
  user_values:
    - path: database.engine
      value: postgres
    - path: database.version
      value: "18"
EOF
```

**`three-tier-placement.yaml`** is a placement policy so Policy Manager routes **`three-tier-app-demo`** to
**`three-tier-provider`**. **`my-petclinic.yaml`** is the catalog instance (Pet Clinic item and DB
settings).

**Control-plane URL:** This repo does not ship a DCM config file. The CLI defaults to
`http://localhost:8080`, which matches this compose stack; **no config file is required** for
that case. Otherwise set `export DCM_CONTROL_PLANE_URL=…` for the session, or create
`~/.dcm/config.yaml`:

```yaml
control-plane-url: http://localhost:8080
```

Legacy aliases `DCM_API_GATEWAY_URL` and `api-gateway-url` still work.

If Policy Manager has never been configured, add the rule below so the
platform selects your three-tier provider (same `name` as in step **1**, usually `three-tier-provider`).
Without it, the instance create call can fail with `policy response missing selected provider`.

Rego sees the instance **`spec`** as **`input.spec`**. This example matches
`service_type == "three-tier-app-demo"`; other service types skip this policy.

```bash
dcm policy create --from-file /tmp/dcm-petclinic/three-tier-placement.yaml --id three-tier-placement
```

Then provision the three tier demo app:

```bash
dcm catalog item list
dcm catalog instance create --from-file /tmp/dcm-petclinic/my-petclinic.yaml
```

#### With `curl`

**First time only (policy):** Same placement rule as above. POST the policy below only if it is not
already present (duplicate returns **409**).

```bash
curl -s -X POST 'http://localhost:8080/api/v1alpha1/policies?id=three-tier-placement' \
  -H 'Content-Type: application/json' \
  -d @- <<'JSON'
{
  "display_name": "Three-tier placement",
  "policy_type": "GLOBAL",
  "enabled": true,
  "priority": 100,
  "rego_code": "package policies.three_tier_default\n\nmain := {\n  \"rejected\": false,\n  \"selected_provider\": \"three-tier-provider\"\n} if {\n  input.spec.service_type == \"three-tier-app-demo\"\n}\n"
}
JSON
```

List catalog items to find the Pet Clinic offering:

```bash
curl -s http://localhost:8080/api/v1alpha1/catalog-items | jq .
```

> **Note:** Look for a catalog item with a `display_name` that indicates a Pet Clinic service. Note its `uid` value.

Create a catalog item instance:

```bash
curl -sS -X POST http://localhost:8080/api/v1alpha1/catalog-item-instances \
  -H "Content-Type: application/json" \
  -d '{
  "api_version": "v1alpha1",
  "display_name": "my-petclinic",
  "spec": {
    "catalog_item_id": "pet-clinic",
    "user_values": [
      { "path": "database.engine", "value": "postgres" },
      { "path": "database.version", "value": "18" }
    ]
  }
}'
```

> **Note:** Catalog Manager only applies `user_values` whose `path` matches an **editable** field on
> that catalog item. For the seeded Pet Clinic offering (`pet-clinic`), only **`database.engine`**
> and **`database.version`** are editable; app and web images use the catalog defaults.

### 3. Verify the Pet Clinic application is running

Monitor the Pet Clinic deployment in Kubernetes:

```bash
kubectl get pods -n default
```

Wait for the Pet Clinic pod(s) to reach `Running` status.

Find the services:

```bash
kubectl get svc -n default
```

The **web** tier is a Service whose name ends with `-web` (HTTP). On Kind, that Service is usually **ClusterIP** only, so your browser cannot reach it directly from the host. Forward a local port to it (adjust the service name and ports to match `kubectl get svc`; web often uses port **80**):

```bash
kubectl port-forward -n default svc/<stack-id>-web 8080:80
```

Then open **http://localhost:8080** in a browser.
Use **Ctrl+C** in the terminal to stop forwarding.

## Troubleshooting

### The three-tier SP fails to start

If **`compose up`** errors or a container exits, inspect:

```bash
podman compose --profile three-tier ps
podman ps -a --format "{{.Names}}\t{{.Status}}" | grep -i three-tier
podman compose --profile three-tier logs --tail=80 three-tier-demo-service-provider
podman compose --profile three-tier logs --tail=80 k8s-container-service-provider
```

Then check logs for a specific container name if needed:

```bash
podman logs <container-name>
```

Common issues:

- **Kubeconfig not mounted correctly:** Verify `K8S_CONTAINER_SP_KUBECONFIG` is set and the file exists.
- **k8s-container-service-provider not running:** Ensure the k8s-container-service-provider is healthy.
- **NATS or Postgres not ready:** Check that `nats` and `postgres` services are running.

### Pet Clinic pod fails to start

Check the pod events:

```bash
kubectl describe pod <pod-name> -n default
```

Check logs:

```bash
kubectl logs <pod-name> -n default
```

### Typical errors and causes

| Problem | Cause |
|---|---|
| `policy response missing selected provider` | No enabled Policy Manager policy sets `selected_provider`, or the value does not match a registered provider `name`. Add the policy in step **2**. |
| `provider '…' is not in ready state (not_ready)` | Service Provider Manager periodically GETs `{endpoint}/health`. If that fails repeatedly, the provider becomes `not_ready` and SPRM rejects provisioning. Ensure the three-tier process responds with 2xx on that path (the demo SP redirects `/health` to `/api/v1alpha1/health`), then wait for the next health check cycle or restart the stack. Verify with `GET /api/v1alpha1/providers` and `health_status: "ready"`. |
| Three-tier SP cannot provision apps without k8s-container-service-provider | The three-tier SP is a high-level orchestration layer that delegates resource provisioning to a k8s-container-service-provider |
| App is unreachable from the host | Kind often exposes the web Service as ClusterIP only. Use `kubectl port-forward` to the **`-web`** Service (step **3**). |
| Deployment hangs or fails | Missing environment variables or unhealthy dependencies (NATS, Postgres, k8s-container-service-provider) |
