# control-plane

Monorepo for the DCM control plane.

## Layout

```text
cmd/control-plane/              # monolith entrypoint (skeleton)
cmd/catalog-manager/            # catalog manager entrypoint (interim)
internal/catalog/               # catalog manager domain
internal/placement/             # placement manager domain (placeholder)
internal/policy/                # policy manager domain (placeholder)
internal/serviceprovider/       # service provider manager domain (placeholder)
api/catalog/                    # catalog OpenAPI and generated types
pkg/catalog/                    # catalog generated client
test/subsystem/catalog/         # catalog subsystem tests
deploy/                         # deployment assets (to be added)
```

See the [control plane monolith enhancement](https://github.com/dcm-project/enhancements/tree/main/enhancements/control-plane-monolith).

## Development

```bash
make build
make build-catalog
make test
make lint
make run-catalog
make check-catalog-aep
```
