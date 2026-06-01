//go:build subsystem

package subsystem_test

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/control-plane/api/placement/v1alpha1"
)

var _ = Describe("Placement API", func() {
	BeforeEach(func() {
		resetPolicyWireMock()
		resetSPRMWireMock()
		stubPolicyEvaluateApproved("test-provider")
		stubSPRMCreateResource()
		stubSPRMDeleteResource()
	})

	Describe("CreateResource", func() {
		It("creates a resource and returns 201", func() {
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2, "memory": "4Gi"},
			}

			resp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(resp.JSON201).NotTo(BeNil())
			Expect(resp.JSON201.Id).NotTo(BeNil())
			Expect(resp.JSON201.CatalogItemInstanceId).To(Equal(body.CatalogItemInstanceId))
			Expect(resp.JSON201.ApprovalStatus).NotTo(BeNil())
			Expect(*resp.JSON201.ApprovalStatus).To(Equal("APPROVED"))
			Expect(resp.JSON201.ProviderName).NotTo(BeNil())
			Expect(*resp.JSON201.ProviderName).To(Equal("test-provider"))

			// Verify clients are called at once
			verifyPolicyEvaluateCalled(1)
			verifySPRMCreateResourceCalled(1)
		})

		It("creates a resource with a user specified ID", func() {
			resourceID := uuid.New().String()
			params := &v1alpha1.CreateResourceParams{Id: &resourceID}
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 4},
			}

			resp, err := apiClient.CreateResourceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(resp.JSON201).NotTo(BeNil())
			Expect(*resp.JSON201.Id).To(Equal(resourceID))
		})

		It("creates a resource with MODIFIED policy status", func() {
			resetPolicyWireMock()
			stubPolicyEvaluateModified("modified-provider", map[string]any{"cpu": 8, "memory": "16Gi"})

			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2, "memory": "4Gi"},
			}

			resp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			Expect(*resp.JSON201.ApprovalStatus).To(Equal("MODIFIED"))
			Expect(*resp.JSON201.ProviderName).To(Equal("modified-provider"))
		})

		It("returns 406 when policy rejects the request", func() {
			resetPolicyWireMock()
			stubPolicyEvaluateRejected()

			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rejected",
				Spec:                  map[string]any{"cpu": 100},
			}

			resp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotAcceptable))

			verifySPRMCreateResourceCalled(0)
		})

		It("returns 500 when policy engine fails", func() {
			resetPolicyWireMock()
			stubPolicyEvaluateFailure()

			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-policy-fail",
				Spec:                  map[string]any{"cpu": 2},
			}

			resp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			verifySPRMCreateResourceCalled(0)
		})

		It("returns 500 and rolls back DB when SPRM fails", func() {
			resetSPRMWireMock()
			stubSPRMCreateResourceFailure()

			catalogID := "catalog-sprm-fail-" + uuid.New().String()[:8]
			body := v1alpha1.Resource{
				CatalogItemInstanceId: catalogID,
				Spec:                  map[string]any{"cpu": 2},
			}

			resp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			verifyPolicyEvaluateCalled(1)
		})

		It("returns 409 when creating resource with duplicate ID", func() {
			resourceID := uuid.New().String()
			params := &v1alpha1.CreateResourceParams{Id: &resourceID}
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			resp1, err := apiClient.CreateResourceWithResponse(context.Background(), params, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp1.StatusCode()).To(Equal(http.StatusCreated))

			// Reset wiremocks for the second call
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateApproved("test-provider")
			stubSPRMCreateResource()

			body2 := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 4},
			}
			resp2, err := apiClient.CreateResourceWithResponse(context.Background(), params, body2)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp2.StatusCode()).To(Equal(http.StatusConflict))
		})
	})

	Describe("GetResource", func() {
		It("retrieves a created resource", func() {
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			resourceID := *createResp.JSON201.Id

			getResp, err := apiClient.GetResourceWithResponse(context.Background(), resourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getResp.JSON200).NotTo(BeNil())
			Expect(*getResp.JSON200.Id).To(Equal(resourceID))
			Expect(getResp.JSON200.CatalogItemInstanceId).To(Equal(body.CatalogItemInstanceId))
		})

		It("returns 404 for non-existent resource", func() {
			resp, err := apiClient.GetResourceWithResponse(context.Background(), "non-existent-id")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})
	})

	Describe("ListResources", func() {
		It("lists created resources", func() {
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-list-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			_, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())

			listResp, err := apiClient.ListResourcesWithResponse(context.Background(), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(listResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(listResp.JSON200).NotTo(BeNil())
			Expect(len(listResp.JSON200.Resources)).To(BeNumerically(">", 0))
		})

		It("paginates results with NextPageToken", func() {
			for i := 0; i < 3; i++ {
				body := v1alpha1.Resource{
					CatalogItemInstanceId: "catalog-page-" + uuid.New().String()[:8],
					Spec:                  map[string]any{"cpu": 2},
				}

				// Reset wiremocks for each creation
				resetPolicyWireMock()
				resetSPRMWireMock()
				stubPolicyEvaluateApproved("test-provider")
				stubSPRMCreateResource()

				resp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode()).To(Equal(http.StatusCreated))
			}

			pageSize := 1
			params := &v1alpha1.ListResourcesParams{MaxPageSize: &pageSize}

			// Fetch first page
			page1, err := apiClient.ListResourcesWithResponse(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())
			Expect(page1.StatusCode()).To(Equal(http.StatusOK))
			Expect(page1.JSON200.Resources).To(HaveLen(1))
			Expect(page1.JSON200.NextPageToken).NotTo(BeNil())
			firstID := *page1.JSON200.Resources[0].Id

			// Fetch second page using the token
			params2 := &v1alpha1.ListResourcesParams{
				MaxPageSize: &pageSize,
				PageToken:   page1.JSON200.NextPageToken,
			}
			page2, err := apiClient.ListResourcesWithResponse(context.Background(), params2)
			Expect(err).NotTo(HaveOccurred())
			Expect(page2.StatusCode()).To(Equal(http.StatusOK))
			Expect(page2.JSON200.Resources).To(HaveLen(1))
			secondID := *page2.JSON200.Resources[0].Id

			// Pages return different resources
			Expect(firstID).NotTo(Equal(secondID))
		})
	})

	Describe("RehydrateResource", func() {
		It("rehydrates a resource and returns 200", func() {
			// Create a resource first
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2, "memory": "4Gi"},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			oldResourceID := *createResp.JSON201.Id

			// Reset wiremocks and set up for rehydration
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateApproved("rehydrated-provider")
			stubSPRMCreateResource()
			stubSPRMDeleteResourceDeferred()

			// Rehydrate the resource
			newResourceID := uuid.New().String()
			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: newResourceID,
			}

			rehydrateResp, err := apiClient.RehydrateResourceWithResponse(context.Background(), oldResourceID, rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(rehydrateResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(rehydrateResp.JSON200).NotTo(BeNil())
			Expect(*rehydrateResp.JSON200.Id).To(Equal(newResourceID))
			Expect(rehydrateResp.JSON200.CatalogItemInstanceId).To(Equal(body.CatalogItemInstanceId))
			Expect(*rehydrateResp.JSON200.ProviderName).To(Equal("rehydrated-provider"))

			// Verify policy was called for re-evaluation
			verifyPolicyEvaluateCalled(1)
			// Verify SPRM create was called for new resource
			verifySPRMCreateResourceCalled(1)
			// Verify SPRM deferred delete was called for old resource
			verifySPRMDeleteResourceDeferredCalled(1)

			// Verify old resource is gone
			getResp, err := apiClient.GetResourceWithResponse(context.Background(), oldResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))

			// Verify new resource exists
			getNewResp, err := apiClient.GetResourceWithResponse(context.Background(), newResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getNewResp.StatusCode()).To(Equal(http.StatusOK))
			Expect(getNewResp.JSON200.CatalogItemInstanceId).To(Equal(body.CatalogItemInstanceId))
		})

		It("returns 404 for non-existent resource", func() {
			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: uuid.New().String(),
			}

			resp, err := apiClient.RehydrateResourceWithResponse(context.Background(), "non-existent-id", rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 406 when policy rejects re-evaluation", func() {
			// Create a resource first
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-reject-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			oldResourceID := *createResp.JSON201.Id

			// Reset and stub policy to reject
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateRejected()

			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: uuid.New().String(),
			}

			resp, err := apiClient.RehydrateResourceWithResponse(context.Background(), oldResourceID, rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotAcceptable))

			// Verify SPRM create was NOT called
			verifySPRMCreateResourceCalled(0)

			// Verify old resource still exists
			getResp, err := apiClient.GetResourceWithResponse(context.Background(), oldResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		})

		It("returns 500 when SPRM creation fails and old resource is preserved", func() {
			// Create a resource first
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-sprm-fail-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			oldResourceID := *createResp.JSON201.Id

			// Reset and stub SPRM create to fail
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateApproved("test-provider")
			stubSPRMCreateResourceFailure()

			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: uuid.New().String(),
			}

			resp, err := apiClient.RehydrateResourceWithResponse(context.Background(), oldResourceID, rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			// Verify old resource still exists
			resetPolicyWireMock()
			resetSPRMWireMock()

			getResp, err := apiClient.GetResourceWithResponse(context.Background(), oldResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		})

		It("returns 409 when new_resource_id collides with an existing placement", func() {
			// Create the first resource
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-conflict-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			oldResourceID := *createResp.JSON201.Id

			// Create a second resource that will collide
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateApproved("test-provider")
			stubSPRMCreateResource()

			body2 := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-conflict2-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 4},
			}

			createResp2, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body2)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp2.StatusCode()).To(Equal(http.StatusCreated))

			existingResourceID := *createResp2.JSON201.Id

			// Try to rehydrate old resource with the existing resource's ID
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateApproved("test-provider")

			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: existingResourceID,
			}

			resp, err := apiClient.RehydrateResourceWithResponse(context.Background(), oldResourceID, rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusConflict))

			// Verify old resource still exists
			resetPolicyWireMock()
			resetSPRMWireMock()

			getResp, err := apiClient.GetResourceWithResponse(context.Background(), oldResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		})

		It("returns 500 when policy engine fails during re-evaluation", func() {
			// Create a resource first
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-policy-fail-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			oldResourceID := *createResp.JSON201.Id

			// Reset and stub policy to return 500
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateFailure()

			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: uuid.New().String(),
			}

			resp, err := apiClient.RehydrateResourceWithResponse(context.Background(), oldResourceID, rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusInternalServerError))

			// Verify SPRM create was NOT called
			verifySPRMCreateResourceCalled(0)

			// Verify old resource still exists
			resetPolicyWireMock()
			resetSPRMWireMock()

			getResp, err := apiClient.GetResourceWithResponse(context.Background(), oldResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		})

		It("succeeds even when SPRM deferred delete fails (graceful degradation)", func() {
			// Create a resource first
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-rehydrate-deferred-fail-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			oldResourceID := *createResp.JSON201.Id

			// Reset and stub SPRM deferred delete to fail
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubPolicyEvaluateApproved("test-provider")
			stubSPRMCreateResource()
			stubSPRMDeleteResourceDeferredFailure()

			newResourceID := uuid.New().String()
			rehydrateBody := v1alpha1.RehydrateRequest{
				NewResourceId: newResourceID,
			}

			resp, err := apiClient.RehydrateResourceWithResponse(context.Background(), oldResourceID, rehydrateBody)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusOK))
			Expect(resp.JSON200).NotTo(BeNil())
			Expect(*resp.JSON200.Id).To(Equal(newResourceID))

			// Verify new resource exists
			resetPolicyWireMock()
			resetSPRMWireMock()

			getNewResp, err := apiClient.GetResourceWithResponse(context.Background(), newResourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getNewResp.StatusCode()).To(Equal(http.StatusOK))
		})
	})

	Describe("DeleteResource", func() {
		It("deletes a resource and returns 204", func() {
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-delete-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			resourceID := *createResp.JSON201.Id

			// Reset wiremocks before delete
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubSPRMDeleteResource()

			deleteResp, err := apiClient.DeleteResourceWithResponse(context.Background(), resourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusNoContent))

			verifySPRMDeleteResourceCalled(1)

			// Verify resource is gone
			getResp, err := apiClient.GetResourceWithResponse(context.Background(), resourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("returns 404 for non-existent resource", func() {
			resp, err := apiClient.DeleteResourceWithResponse(context.Background(), "non-existent-id")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode()).To(Equal(http.StatusNotFound))
		})

		It("preserves DB record when SPRM delete fails", func() {
			body := v1alpha1.Resource{
				CatalogItemInstanceId: "catalog-sprm-del-fail-" + uuid.New().String()[:8],
				Spec:                  map[string]any{"cpu": 2},
			}

			createResp, err := apiClient.CreateResourceWithResponse(context.Background(), nil, body)
			Expect(err).NotTo(HaveOccurred())
			Expect(createResp.StatusCode()).To(Equal(http.StatusCreated))

			resourceID := *createResp.JSON201.Id

			// Reset wiremocks and stub SPRM failure
			resetPolicyWireMock()
			resetSPRMWireMock()
			stubSPRMDeleteResourceFailure()

			deleteResp, err := apiClient.DeleteResourceWithResponse(context.Background(), resourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteResp.StatusCode()).To(Equal(http.StatusInternalServerError))

			// Verify the resource is still in the DB
			getResp, err := apiClient.GetResourceWithResponse(context.Background(), resourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(getResp.StatusCode()).To(Equal(http.StatusOK))
		})
	})
})
