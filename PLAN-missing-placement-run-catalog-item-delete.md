# Plan: Catalog item deletion after its Placement run is missing

## Finding

Upstream PR [#75](https://github.com/dcm-project/control-plane/pull/75), “fix(catalog): treat missing placement run as deleted,” appears to fix the reported Control Plane failure. It preserves the Placement Manager HTTP status, treats a structured 404 from `DeleteRun` as already cleaned up, and deletes the local catalog-item instance. Its subsystem test checks successful instance deletion and subsequent catalog-item cleanup.

The PR is still open. Its description references FLPATH-4925, while this report cites FLPATH-4752. The implementation matches the reported failure path, but the Jira association should be made explicit before considering FLPATH-4752 formally covered.

## Validation of PR #75 against the reported path

- The placement deletion callback removes completed resources for the run. A later `PlacementService.DeleteRun` sees no resources and returns its normal not-found service error (`internal/placement/service/callbacks.go` and `internal/placement/service/placement.go`).
- The monolith defaults to the in-process catalog Placement client (`internal/app/run.go`). That client maps the Placement service's not-found error to `*placement.PlacementError{StatusCode: 404}` (`internal/catalog/placement/local_client.go`). The HTTP Placement client also returns a typed `PlacementError` after PR #75's change. Therefore the service's typed-404 branch in PR #75 handles both configured client paths, including the embedded-provider setup.
- After that branch accepts the missing run, the service continues to delete the local catalog-item-instance row. This addresses the reported orphan and allows catalog-item cleanup.
- I ran `go test ./internal/catalog/placement ./internal/catalog/service` at PR #75 head `884a6e4`; both packages passed. I did not run the subsystem stack or the full embedded-provider scenario. PR #75's subsystem test uses a WireMock 404 and verifies instance DELETE 204, instance GET 404, and catalog-item DELETE 204.

**Validation result:** No remaining functional fix was identified, so a follow-up code PR is not warranted. The remaining gap is verification breadth: PR #75 does not directly test the in-process client mapping or drive the full embedded-provider acknowledgement sequence. The existing FLPATH-4752 TC-05 scenario should be rerun after PR #75 is merged. Separately, make the Jira relationship explicit because PR #75 currently references FLPATH-4925.

## Proposed work

1. Review PR #75 as the candidate fix; do not create a duplicate implementation.
2. Confirm the PR only treats a structured 404 from the catalog delete’s `DeleteRun` call as success. Other Placement Manager failures must still fail and leave the local catalog-item instance available for retry.
3. Confirm the PR checks pass. The PR description reports passing catalog and placement service tests, a focused subsystem regression, and `git diff --check`; these were not rerun during this analysis.
4. Associate PR #75 with FLPATH-4752 if that is the intended Jira issue. The PR currently references FLPATH-4925.
5. Validate the complete FLPATH-4752 TC-05 flow with the embedded VM provider after the fix is available. The PR subsystem test stubs the Placement Manager 404 rather than driving the real provider acknowledgement sequence.
6. Merge the existing PR after its review and checks pass.

## Expected result

- Deleting the catalog-item instance after its Placement run is gone returns HTTP 204 and removes the local catalog-item-instance record.
- A subsequent GET for that instance returns HTTP 404.
- Deleting the associated catalog item succeeds.
- Non-404 Placement Manager failures retain current error handling and do not remove the local instance.
- Unknown catalog-item-instance deletion retains its current HTTP 404 behavior.

## Scope

Keep the fix in the Control Plane catalog delete path. Do not change the Environment Agent or provider missing-resource acknowledgement behavior, which the report says already passed. No separate feature spec or test-plan file is needed; the existing API contract already documents the DELETE success response, and PR #75 adds unit and subsystem coverage.

## Verification commands

- Catalog unit tests: `make test-catalog`
- Catalog subsystem tests: `make catalog-subsystem-test-up`, `make catalog-subsystem-test`, then `make catalog-subsystem-test-down`
- Product-level validation: rerun FLPATH-4752 TC-05 with the embedded VM provider.
