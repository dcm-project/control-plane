# control-plane

Monorepo for the DCM control plane monolith.

## Layout

```text
cmd/control-plane/              # monolith entrypoint
internal/app/                   # wiring, config, shared DB bootstrap
internal/catalog/               # catalog manager
internal/placement/             # placement manager
internal/policy/                # policy manager (CRUD + OPA engine)
internal/sp/                    # service provider manager
api/                            # OpenAPI specs per domain
pkg/                            # generated HTTP clients
test/subsystem/                 # per-domain subsystem tests
deploy/                         # compose and postgres init
```

See the [control plane monolith enhancement](https://github.com/dcm-project/enhancements/tree/main/enhancements/control-plane-monolith).

## Development

Build and unit tests:

```bash
make build
make test
make lint
make check-catalog-aep
```

Run the monolith (pick one):

| Command | Where it runs | Database | Other deps |
|---------|---------------|----------|------------|
| `make run` | host | SQLite at `/tmp/control-plane.db` | NATS disabled |
| `make run-dev` | host | Postgres (`DB_*` defaults) | Postgres + NATS running locally |
| `make compose-up` | containers | Postgres in compose | also starts NATS and control-plane |

```bash
make run              # SQLite, no containers
make compose-up       # full stack in containers
make compose-down     # stop stack and remove volumes
```

Compose uses `POSTGRES_USER` and `POSTGRES_PASSWORD` (defaults in compose
are for local dev only). Override via environment or a `.env` file; see
`.env.example`.

Policy evaluation and placement provisioning run in-process in the monolith
(`EvaluationService`, `PlacementService` via local clients). There is no public
HTTP route for `policies:evaluateRequest` or `/resources`. Use
`make test-policy` and `make test-placement` for unit coverage.

Per-domain subsystem tests still run separately; catalog compose may set
`PLACEMENT_MANAGER_URL` to reach WireMock placement stubs (test-only; not a
production API on the monolith).

Legacy `cmd/*-manager` binaries were removed. Use `make build` / `make run`.

### Container image

Build locally:

```bash
make image-build
```

CI pushes to `quay.io/dcm-project/control-plane` on merges to `main` and
`release/v*` branches (and on version tags). See
[Releasing](https://github.com/dcm-project/shared-workflows#release-flow)
in shared-workflows for tag behavior and version conventions.
