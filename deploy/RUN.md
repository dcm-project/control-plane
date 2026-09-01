# Running DCM

## Prerequisites

- [Podman](https://podman.io/) or [Docker](https://www.docker.com/) (the Makefile auto-detects which engine is available)
- (Optional) [Kind](https://kind.sigs.k8s.io/) with KubeVirt for the environment-agent embedded `vm` SP
- (Optional) A Kubernetes cluster for environment-agent embedded `container` and `cluster` SPs
- (Optional) [utilities](https://github.com/dcm-project/utilities) repo as a sibling directory for Kind helper scripts (`../utilities`)

## Quick start

### With environment-agent (Service Providers)

An [environment-agent](https://github.com/dcm-project/environment-agent) must be running and
registered with the control-plane in order to use Service Providers. Follow the guide,
[environment-agent-kind.md](docs/environment-agent-kind.md) for the full setup.

### Control-plane and UI only

If you only need the API and UI (no workload provisioning):

```bash
make compose-up
```

The control-plane API is at `http://localhost:8080`. DCM UI is at `http://localhost:7007`.

Authentication is **disabled by default** (`AUTH_DISABLED=true`). See
[Authentication](#authentication) for enabling it and current limitations.

## CLI configuration

The [DCM CLI](https://github.com/dcm-project/cli) uses the same control-plane URL by default
(`http://localhost:8080`). Override it with the `control-plane-url` key in `~/.dcm/config.yaml`
or the `DCM_CONTROL_PLANE_URL` environment variable. See the [CLI README](https://github.com/dcm-project/cli/blob/main/README.md)
for install and usage.

The CLI forwards bearer tokens to the control-plane API. Run `dcm login` for interactive
OIDC device authorization (Keycloak `dcm-cli` client), or set `DCM_TOKEN` / `--token`
for CI and scripting.

## Running with Environment Agent

See [environment-agent-kind.md](docs/environment-agent-kind.md) for Kind setup, the
`environment-agent` compose profile, configuration, and verification.

## Authentication

The compose stack includes [Keycloak](https://www.keycloak.org/) (`:8180`) as the identity
provider. The control-plane validates JWT bearer tokens directly against Keycloak's
JWKS endpoint using OIDC discovery (no external auth proxy required). A proxy-header
fallback path (`X-Auth-Proxy-Secret` + `X-Forwarded-User`) is also supported.

Authentication is disabled by default (`AUTH_DISABLED=true`). When enabled, the CLI
(`dcm login` / bearer token) and direct JWT API calls work; the environment-agent and legacy
external service providers do not forward authentication headers yet, so SP workflows may fail.

To enable authentication (Compose):

```bash
AUTH_DISABLED=false AUTH_ISSUER_URL=http://keycloak:8080/realms/dcm make compose-up
```

For Helm chart installs, see [helm/dcm/README.md](helm/dcm/README.md#authentication)
(`auth.enabled=true`).

> **Warning:** The environment-agent and legacy external service providers do not forward
> authentication headers yet, so enabling auth can break SP workflows. The CLI (`dcm login` /
> bearer token) and direct API calls with a valid Keycloak JWT work.

When enabled, the control-plane authenticates requests via two paths (tried in order):

1. **JWT bearer token** (primary): `Authorization: Bearer <token>` — validated against Keycloak JWKS. Requires `AUTH_ISSUER_URL` to be set.
2. **Proxy headers** (fallback): `X-Auth-Proxy-Secret` + `X-Forwarded-User` — for callers routing through an auth proxy. Requires `AUTH_PROXY_SECRET` to be set.

The `/api/v1alpha1/health` endpoint is always unauthenticated.

Pre-configured credentials (local dev only):

| Service | URL | Username | Password |
|---|---|---|---|
| Keycloak admin console | `http://localhost:8180` | `admin` | `admin` |
| DCM user (Keycloak) | — | `dcm-admin` | `admin` |

The Keycloak realm is imported from `deploy/keycloak/realm-export.json` and includes
two clients: `dcm-proxy` (confidential, for service-to-service access) and `dcm-cli`
(public, for the DCM CLI device auth grant flow).

`DCM_ADMIN_SUBJECT` must match the `id` of a user in the Keycloak realm. The compose
default (`56deb662-4820-5d83-b828-f4beb11a5fa7`) corresponds to the pre-configured
`dcm-admin` user.

### Adding users

Create users in the Keycloak admin console at `http://localhost:8180` (login with
`admin` / `admin`). Navigate to the `dcm` realm, **Users → Add user**, fill in
a username, save, then set a password under the **Credentials** tab (disable
"Temporary"). Users must be in the `dcm` realm — the control-plane's OIDC
configuration points to this realm.

New users are automatically provisioned in the control-plane on first
authenticated request (JIT provisioning) — no manual DB setup is required.

## Verifying the deployment

Check that all services are running:

```bash
podman compose -f deploy/compose.yaml ps    # or: docker compose -f deploy/compose.yaml ps
```

Check the health endpoint (unauthenticated, works regardless of `AUTH_DISABLED`):

```bash
curl http://localhost:8080/api/v1alpha1/health
```

Check health endpoint through DCM UI:

```bash
curl http://localhost:7007/api/dcm/health
```

When authentication is enabled, verify Keycloak is ready:

```bash
podman compose -f deploy/compose.yaml exec keycloak curl -sf http://localhost:9000/health/ready | jq .
```

## Stopping services

```bash
make compose-down
```

This stops all compose services and removes volumes. If Kind was connected to
the compose network (see [k8s-container-sp-kind.md](docs/k8s-container-sp-kind.md)),
`compose-down` disconnects external containers and removes both
`control-plane_default` and legacy `deploy_default` networks.

## Configuration

| Variable                                   | Default                     | Description                                                                                                 |
| ------------------------------------------ | --------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `AUTH_DISABLED`                             | `true`                      | Disable authentication (default `true`; see [Authentication](#authentication))                              |
| `AUTH_ISSUER_URL`                           | _(empty)_                   | OIDC issuer URL for JWT validation (e.g. `http://keycloak:8080/realms/dcm`). Empty = JWT path disabled.     |
| `AUTH_JWT_AUDIENCE`                         | _(empty)_                   | Expected `aud` claim in JWT tokens. Empty = audience check skipped.                                         |
| `AUTH_PROXY_SECRET`                         | `dcm-dev-proxy-secret`      | Shared secret for proxy-header fallback auth path                                                           |
| `AUTH_CACHE_TTL`                            | `60s`                       | TTL for the actor resolution cache                                                                          |
| `DCM_ADMIN_SUBJECT`                        | `56deb662-...` _(see below)_ | Keycloak subject UUID for the bootstrap admin actor (required when auth enabled)                            |
| `KEYCLOAK_ADMIN_PASSWORD`                  | `admin`                     | Keycloak admin console password                                                                             |
| `DCM_DEV_USER_PASSWORD`                     | `admin`                     | Password for the `dcm-admin` dev user in Keycloak                                                           |
| `POSTGRES_USER`                            | `admin`                     | PostgreSQL username                                                                                         |
| `POSTGRES_PASSWORD`                        | `adminpass`                 | PostgreSQL password                                                                                         |
| `KUBERNETES_NAMESPACE`                     | `default`                   | Kubernetes namespace for KubeVirt VMs (legacy kubevirt profile)                                           |
| `KUBEVIRT_KUBECONFIG`                      | `~/.kube/config`            | Path to kubeconfig on the host (legacy kubevirt profile)                                                    |
| `AGENT_NAME`                               | `local-agent`               | Agent name for environment-agent profile                                                                    |
| `AGENT_ENVIRONMENT`                        | `dev`                       | Environment classification for environment-agent                                                            |
| `AGENT_COST`                               | `low`                       | Cost classification for environment-agent                                                                   |
| `AGENT_PORT`                               | `8081`                      | Host port for environment-agent HTTP API                                                                    |
| `AGENT_EMBEDDED_SPS`                       | `container,vm`              | Embedded SP types for environment-agent: `container`, `vm`, `cluster`                                       |
| `AGENT_KUBECONFIG_HOST`                    | `~/.kube/config`            | Host kubeconfig bind mount; use `.kube/config` in `deploy/.env` with Kind (`make kubeconfig-for-compose`) |
| `SP_K8S_NAMESPACE`                         | `default`                   | Container SP workload namespace (environment-agent)                                                         |
| `SP_K8S_EXTERNAL_SVC_TYPE`                 | `NodePort`                  | Container SP external service type (environment-agent)                                                      |
| `SP_CLUSTER_NAMESPACE`                     | _(required for cluster SP)_ | ACM cluster namespace (environment-agent cluster SP)                                                        |
| `SP_PULL_SECRET`                           | _(required for cluster SP)_ | Base64-encoded dockerconfigjson for environment-agent cluster SP                                            |
| `SP_BASE_DOMAIN`                           | _(none)_                    | Base domain for hosted clusters (environment-agent cluster SP)                                              |
| `ENVIRONMENT_AGENT_VERSION`                | `main`                      | Image tag for environment-agent                                                                             |
| `KUBEVIRT_PROVIDER_NAME`                   | `kubevirt-service-provider` | Provider name and Compose service `container_name` (legacy kubevirt profile)                                |
| `K8S_CONTAINER_SP_KUBECONFIG`              | `~/.kube/config`            | Path to kubeconfig on the host for the k8s-container-service-provider                                       |
| `K8S_CONTAINER_SP_NAMESPACE`               | `default`                   | Kubernetes namespace for k8s containers                                                                     |
| `K8S_CONTAINER_SP_NAME`                    | `k8s-container-provider`    | Provider name for the k8s-container-service-provider                                                        |
| `K8S_CONTAINER_SP_EXTERNAL_SVC_TYPE`       | `NodePort`                  | Kubernetes Service type for external ports (`NodePort` or `LoadBalancer`)                                   |
| `K8S_STORAGE_SP_KUBECONFIG`                | `~/.kube/config`            | Path to kubeconfig on the host for the k8s-storage-service-provider                                         |
| `K8S_STORAGE_SP_NAMESPACE`                 | `default`                   | Kubernetes namespace used by the k8s-storage-service-provider                                               |
| `K8S_STORAGE_SP_NAME`                      | `k8s-storage-provider`      | Provider name for the k8s-storage-service-provider                                                          |
| `K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS`     | _(empty)_                   | Optional fallback StorageClass when request hints do not set one                                            |
| `K8S_STORAGE_SP_DEFAULT_ACCESS_MODE`       | `ReadWriteOnce`             | Optional fallback access mode when request hints do not set one                                             |
| `ACM_CLUSTER_SP_KUBECONFIG`                | `~/.kube/config`            | Path to kubeconfig on the host for the acm-cluster-service-provider                                         |
| `ACM_CLUSTER_SP_NAMESPACE`                 | `default`                   | Kubernetes namespace for ACM hosted clusters                                                                |
| `ACM_CLUSTER_SP_NAME`                      | `acm-cluster-sp`            | Provider name for the acm-cluster-service-provider                                                          |
| `ACM_CLUSTER_SP_BASE_DOMAIN`               | _(none)_                    | Base DNS domain for hosted clusters; can be overridden per-request via `provider_hints.acm.base_domain`     |
| `ACM_CLUSTER_SP_PULL_SECRET`               | _(required)_                | Base64-encoded dockerconfigjson pull secret for ACM hosted clusters                                         |
| `ACM_CLUSTER_SP_DEFAULT_INFRA_ENV`         | _(none)_                    | **BareMetal only.** Default InfraEnv name; can be overridden per-request via `provider_hints.acm.infra_env` |
| `ACM_CLUSTER_SP_AGENT_NAMESPACE`           | _(none)_                    | **BareMetal only.** Namespace where Agent resources are located                                             |
| `CONTROL_PLANE_VERSION`                    | `main`                      | Image tag for control-plane monolith                                                                        |
| `KUBEVIRT_SERVICE_PROVIDER_VERSION`        | `main`                      | Image tag for kubevirt-service-provider                                                                     |
| `K8S_CONTAINER_SERVICE_PROVIDER_VERSION`   | `main`                      | Image tag for k8s-container-service-provider                                                                |
| `K8S_STORAGE_SERVICE_PROVIDER_VERSION`     | `main`                      | Image tag for k8s-storage-service-provider                                                                  |
| `ACM_CLUSTER_SERVICE_PROVIDER_VERSION`     | `main`                      | Image tag for acm-cluster-service-provider                                                                  |
| `THREE_TIER_DEMO_SERVICE_PROVIDER_VERSION` | `main`                      | Image tag for three-tier-demo-service-provider                                                              |
| `THREE_TIER_SP_NAME`                       | `three-tier-provider`       | Provider name for the three-tier-demo-service-provider                                                      |
| `DCM_UI_VERSION`                           | `main`                      | Image tag for dcm-ui                                                                                        |

See [Image versions](../README.md#image-versions) in the README for available tag formats and how to update.

## Kubernetes / OpenShift

See [helm/dcm/README.md](helm/dcm/README.md).
