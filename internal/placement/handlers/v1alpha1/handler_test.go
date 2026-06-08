package v1alpha1_test

import (
	"context"

	apiv1alpha1 "github.com/dcm-project/control-plane/api/placement/v1alpha1"
	"github.com/dcm-project/control-plane/internal/placement/api/server"
	v1alpha1 "github.com/dcm-project/control-plane/internal/placement/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/placement/policy"
	"github.com/dcm-project/control-plane/internal/placement/service"
	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// mockPolicyClient is a mock implementation of policy.Client for testing
type mockPolicyClient struct {
	EvaluateFunc func(ctx context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error)
}

// Evaluate calls the mock function if set, otherwise returns a default approved response
func (m *mockPolicyClient) Evaluate(ctx context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
	if m.EvaluateFunc != nil {
		return m.EvaluateFunc(ctx, req)
	}
	// Default: approve with the original spec
	return &policy.EvaluateResponse{
		Status:           "APPROVED",
		SelectedProvider: "default-provider",
		EvaluatedSpec:    req.Spec,
	}, nil
}

// mockSPRMClient is a mock implementation of sprm.Client for testing
type mockSPRMClient struct {
	CreateResourceFunc         func(ctx context.Context, req sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error)
	DeleteResourceFunc         func(ctx context.Context, resourceId string) error
	DeleteResourceDeferredFunc func(ctx context.Context, resourceId string) error
}

// CreateResource calls the mock function if set, otherwise returns a default success response
func (m *mockSPRMClient) CreateResource(ctx context.Context, req sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
	if m.CreateResourceFunc != nil {
		return m.CreateResourceFunc(ctx, req)
	}
	// Default: successful creation
	return &sprm.CreateResourceResponse{
		ID:     req.ID,
		Status: "pending",
	}, nil
}

// DeleteResource calls the mock function if set, otherwise returns success
func (m *mockSPRMClient) DeleteResource(ctx context.Context, resourceId string) error {
	if m.DeleteResourceFunc != nil {
		return m.DeleteResourceFunc(ctx, resourceId)
	}
	return nil
}

// DeleteResourceDeferred calls the mock function if set, otherwise returns success
func (m *mockSPRMClient) DeleteResourceDeferred(ctx context.Context, resourceId string) error {
	if m.DeleteResourceDeferredFunc != nil {
		return m.DeleteResourceDeferredFunc(ctx, resourceId)
	}
	return nil
}

var _ = Describe("Handler", func() {
	var (
		db         *gorm.DB
		handler    *v1alpha1.Handler
		ctx        context.Context
		mockPolicy *mockPolicyClient
		mockSPRM   *mockSPRMClient
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Resource{})).To(Succeed())

		dataStore := store.NewStore(db)
		mockPolicy = &mockPolicyClient{}
		mockSPRM = &mockSPRMClient{}
		placementService := service.NewPlacementService(dataStore, mockPolicy, mockSPRM)
		handler = v1alpha1.NewHandler(placementService)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("CreateResource", func() {
		It("creates and returns 201", func() {
			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-item-123",
					Spec:                  map[string]interface{}{"cpu": 2, "memory": "4GB"},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.CreateResource201JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.CatalogItemInstanceId).To(Equal("catalog-item-123"))
			Expect(jsonResp.Id).NotTo(BeNil())
			Expect(jsonResp.Path).NotTo(BeNil())
			Expect(jsonResp.Spec).To(HaveLen(2))
			Expect(jsonResp.Spec).To(HaveKey("cpu"))
			Expect(jsonResp.Spec).To(HaveKey("memory"))
			Expect(jsonResp.Spec["memory"]).To(Equal("4GB"))

			// Verify policy response fields are set
			Expect(jsonResp.ApprovalStatus).NotTo(BeNil())
			Expect(*jsonResp.ApprovalStatus).To(Equal("APPROVED"))
			Expect(jsonResp.ProviderName).NotTo(BeNil())
			Expect(*jsonResp.ProviderName).To(Equal("default-provider"))
		})

		It("creates with specified ID", func() {
			specifiedID := uuid.New().String()
			req := server.CreateResourceRequestObject{
				Params: apiv1alpha1.CreateResourceParams{Id: &specifiedID},
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-item-456",
					Spec:                  map[string]interface{}{"cpu": 1},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.CreateResource201JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Id).To(Equal(specifiedID))

			// Verify policy response fields are set
			Expect(jsonResp.ApprovalStatus).NotTo(BeNil())
			Expect(*jsonResp.ApprovalStatus).To(Equal("APPROVED"))
			Expect(jsonResp.ProviderName).NotTo(BeNil())
			Expect(*jsonResp.ProviderName).To(Equal("default-provider"))
		})

		It("returns 409 for duplicate ID", func() {
			specifiedID := uuid.New().String()
			req := server.CreateResourceRequestObject{
				Params: apiv1alpha1.CreateResourceParams{Id: &specifiedID},
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-item-100",
					Spec:                  map[string]interface{}{"cpu": 1},
				},
			}

			// Create first resource
			resp1, err := handler.CreateResource(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			_, ok := resp1.(server.CreateResource201JSONResponse)
			Expect(ok).To(BeTrue(), "First create should return 201")

			// Try to create second resource with same ID
			resp, err := handler.CreateResource(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Check what we actually got
			if resp201, ok := resp.(server.CreateResource201JSONResponse); ok {
				Fail("Got 201 response instead of 409. Resource ID: " + *resp201.Id)
			}

			problemResp, ok := resp.(server.CreateResource409ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Second create should return 409")
			Expect(problemResp.Type).To(Equal("resource-conflict"))
			Expect(problemResp.Title).To(Equal("Resource already exists"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(409))
			Expect(problemResp.Detail).NotTo(BeNil())
			Expect(*problemResp.Detail).To(ContainSubstring("already exists"))
		})

		It("returns 500 for internal errors", func() {
			// Close the database to simulate internal error
			sqlDB, _ := db.DB()
			_ = sqlDB.Close()

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-item-789",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.CreateResourcedefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(problemResp.StatusCode).To(Equal(500))
			Expect(problemResp.Body.Type).To(Equal("internal-error"))
			Expect(problemResp.Body.Title).To(Equal("Internal error"))
			Expect(problemResp.Body.Status).NotTo(BeNil())
			Expect(*problemResp.Body.Status).To(Equal(500))
		})

		It("handles policy MODIFIED status and sets approval status", func() {
			var capturedEvaluatedSpec map[string]any
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				// Verify input spec
				Expect(req.Spec).To(HaveKey("cpu"))
				Expect(req.Spec["cpu"]).To(Equal(2))

				// Modify the spec by adding a field
				modifiedSpec := make(map[string]any)
				for k, v := range req.Spec {
					modifiedSpec[k] = v
				}
				modifiedSpec["modified_by_policy"] = true
				capturedEvaluatedSpec = modifiedSpec

				return &policy.EvaluateResponse{
					Status:           "MODIFIED",
					SelectedProvider: "policy-selected-provider",
					EvaluatedSpec:    modifiedSpec,
				}, nil
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-modified",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.CreateResource201JSONResponse)
			Expect(ok).To(BeTrue())

			// Verify the evaluated spec was generated by policy
			Expect(capturedEvaluatedSpec).NotTo(BeNil())
			Expect(capturedEvaluatedSpec).To(HaveKey("cpu"))
			Expect(capturedEvaluatedSpec).To(HaveKey("modified_by_policy"))
			Expect(capturedEvaluatedSpec["modified_by_policy"]).To(BeTrue())

			// Original spec should remain unchanged in the response
			Expect(jsonResp.Spec).To(HaveKey("cpu"))
			Expect(jsonResp.Spec).NotTo(HaveKey("modified_by_policy"))
			Expect(jsonResp.Spec["cpu"]).To(Equal(float64(2)))

			// Approval status should be set to MODIFIED
			Expect(jsonResp.ApprovalStatus).NotTo(BeNil())
			Expect(*jsonResp.ApprovalStatus).To(Equal("MODIFIED"))
			Expect(*jsonResp.ProviderName).To(Equal("policy-selected-provider"))
		})

		It("handles policy APPROVED status", func() {
			var capturedEvaluatedSpec map[string]any
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				// Verify input spec
				Expect(req.Spec).To(HaveKey("cpu"))
				Expect(req.Spec["cpu"]).To(Equal(4))

				// Return the same spec without modifications (approved as-is)
				capturedEvaluatedSpec = req.Spec

				return &policy.EvaluateResponse{
					Status:           "APPROVED",
					SelectedProvider: "approved-provider",
					EvaluatedSpec:    req.Spec,
				}, nil
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-approved",
					Spec:                  map[string]interface{}{"cpu": 4},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.CreateResource201JSONResponse)
			Expect(ok).To(BeTrue())

			// Verify the evaluated spec is the same as input
			Expect(capturedEvaluatedSpec).NotTo(BeNil())
			Expect(capturedEvaluatedSpec).To(HaveKey("cpu"))
			Expect(capturedEvaluatedSpec["cpu"]).To(Equal(4))
			Expect(capturedEvaluatedSpec).To(HaveLen(1)) // No additional fields

			// Original spec should remain unchanged in the response
			Expect(jsonResp.Spec).To(HaveKey("cpu"))
			Expect(jsonResp.Spec["cpu"]).To(Equal(float64(4)))
			Expect(jsonResp.Spec).To(HaveLen(1))

			// Approval status should be set to APPROVED
			Expect(jsonResp.ApprovalStatus).NotTo(BeNil())
			Expect(*jsonResp.ApprovalStatus).To(Equal("APPROVED"))
			Expect(*jsonResp.ProviderName).To(Equal("approved-provider"))
		})

		It("returns 406 when policy rejects request", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 406, Body: "rejected by policy"}
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-rejected",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.CreateResource406ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 406 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("policy-rejected"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(406))
		})

		It("returns 409 when policy conflict detected", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 409, Body: "policy conflict"}
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-conflict",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.CreateResource409ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(problemResp.Type).To(Equal("policy-conflict"))
		})

		It("returns 500 when policy engine internal error", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 500, Body: "internal server error"}
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-error",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			defaultResp, ok := resp.(server.CreateResourcedefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(defaultResp.StatusCode).To(Equal(500))
			Expect(defaultResp.Body.Type).To(Equal("internal-error"))
		})

		It("returns 500 when SPRM internal error", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 500, Body: "SPRM internal server error"}
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-sprm-error",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			defaultResp, ok := resp.(server.CreateResourcedefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(defaultResp.StatusCode).To(Equal(500))
			Expect(defaultResp.Body.Type).To(Equal("internal-error"))
			Expect(*defaultResp.Body.Detail).To(ContainSubstring("SPRM internal error"))
		})

		It("returns 400 when policy validation fails", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 400, Body: "bad request"}
			}

			req := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-invalid",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}

			resp, err := handler.CreateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.CreateResource400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
			Expect(problemResp.Type).To(Equal("validation-error"))
		})
	})

	Describe("GetResource", func() {
		It("returns resource", func() {
			// Create a resource first
			createReq := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-instance-1234",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}
			createResp, _ := handler.CreateResource(ctx, createReq)
			created := createResp.(server.CreateResource201JSONResponse)

			req := server.GetResourceRequestObject{
				ResourceId: *created.Id,
			}

			resp, err := handler.GetResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.GetResource200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.CatalogItemInstanceId).To(Equal("catalog-instance-1234"))
		})

		It("returns 404 for non-existent resource", func() {
			req := server.GetResourceRequestObject{
				ResourceId: uuid.New().String(),
			}

			resp, err := handler.GetResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.GetResource404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("ListResources", func() {
		It("returns empty list initially", func() {
			req := server.ListResourcesRequestObject{}

			resp, err := handler.ListResources(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.Resources).To(BeEmpty())
		})

		It("returns resources", func() {
			// Create resources first
			for i := 0; i < 3; i++ {
				createReq := server.CreateResourceRequestObject{
					Body: &apiv1alpha1.Resource{
						CatalogItemInstanceId: "catalog-instance-123",
						Spec:                  map[string]interface{}{"cpu": i + 1},
					},
				}
				_, err := handler.CreateResource(ctx, createReq)
				Expect(err).NotTo(HaveOccurred())
			}

			resp, err := handler.ListResources(ctx, server.ListResourcesRequestObject{})

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.Resources).To(HaveLen(3))
		})

		It("respects max page size and returns next page token", func() {
			// Create 5 resources
			for i := 0; i < 5; i++ {
				createReq := server.CreateResourceRequestObject{
					Body: &apiv1alpha1.Resource{
						CatalogItemInstanceId: "catalog-instance-123",
						Spec:                  map[string]interface{}{"cpu": i + 1},
					},
				}
				_, err := handler.CreateResource(ctx, createReq)
				Expect(err).NotTo(HaveOccurred())
			}

			// First page: request 2 items
			maxPageSize := 2
			req := server.ListResourcesRequestObject{
				Params: apiv1alpha1.ListResourcesParams{MaxPageSize: &maxPageSize},
			}

			resp, err := handler.ListResources(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.ListResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.Resources).To(HaveLen(2))
			Expect(jsonResp.NextPageToken).NotTo(BeNil())
			Expect(*jsonResp.NextPageToken).NotTo(BeEmpty())

			// Second page: use the page token
			req2 := server.ListResourcesRequestObject{
				Params: apiv1alpha1.ListResourcesParams{
					MaxPageSize: &maxPageSize,
					PageToken:   jsonResp.NextPageToken,
				},
			}

			resp2, err := handler.ListResources(ctx, req2)

			Expect(err).NotTo(HaveOccurred())
			jsonResp2, ok := resp2.(server.ListResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp2.Resources).To(HaveLen(2))
			Expect(jsonResp2.NextPageToken).NotTo(BeNil())

			// Third page: should have 1 item and no next token
			req3 := server.ListResourcesRequestObject{
				Params: apiv1alpha1.ListResourcesParams{
					MaxPageSize: &maxPageSize,
					PageToken:   jsonResp2.NextPageToken,
				},
			}

			resp3, err := handler.ListResources(ctx, req3)

			Expect(err).NotTo(HaveOccurred())
			jsonResp3, ok := resp3.(server.ListResources200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp3.Resources).To(HaveLen(1))
			Expect(jsonResp3.NextPageToken).To(BeNil())
		})

		It("returns 400 for invalid page size", func() {
			invalidPageSize := 200
			req := server.ListResourcesRequestObject{
				Params: apiv1alpha1.ListResourcesParams{MaxPageSize: &invalidPageSize},
			}

			resp, err := handler.ListResources(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.ListResources400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("DeleteResource", func() {
		It("deletes resource and returns 204", func() {
			// Create a resource first
			createReq := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-delete",
					Spec:                  map[string]interface{}{"cpu": 2},
				},
			}
			createResp, _ := handler.CreateResource(ctx, createReq)
			created := createResp.(server.CreateResource201JSONResponse)

			req := server.DeleteResourceRequestObject{
				ResourceId: *created.Id,
			}

			resp, err := handler.DeleteResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.DeleteResource204Response)
			Expect(ok).To(BeTrue())

			// Verify it's deleted
			getResp, _ := handler.GetResource(ctx, server.GetResourceRequestObject{ResourceId: *created.Id})
			_, ok = getResp.(server.GetResource404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 404 for non-existent resource", func() {
			req := server.DeleteResourceRequestObject{
				ResourceId: uuid.New().String(),
			}

			resp, err := handler.DeleteResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(server.DeleteResource404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("RehydrateResource", func() {
		var oldResourceID string

		BeforeEach(func() {
			// Create a resource to rehydrate
			createReq := server.CreateResourceRequestObject{
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-rehydrate",
					Spec:                  map[string]interface{}{"cpu": 2, "memory": "4GB"},
				},
			}
			createResp, err := handler.CreateResource(ctx, createReq)
			Expect(err).NotTo(HaveOccurred())
			created := createResp.(server.CreateResource201JSONResponse)
			oldResourceID = *created.Id
		})

		It("rehydrates and returns 200", func() {
			newResourceID := uuid.New().String()
			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: newResourceID},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(server.RehydrateResource200JSONResponse)
			Expect(ok).To(BeTrue(), "Expected 200 response but got: %T", resp)
			Expect(jsonResp.Id).NotTo(BeNil())
			Expect(*jsonResp.Id).NotTo(Equal(oldResourceID))
			Expect(jsonResp.CatalogItemInstanceId).To(Equal("catalog-rehydrate"))
			Expect(jsonResp.Spec).To(HaveKey("cpu"))
			Expect(jsonResp.Spec).To(HaveKey("memory"))
		})

		It("returns 404 for non-existent resource", func() {
			req := server.RehydrateResourceRequestObject{
				ResourceId: uuid.New().String(),
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.RehydrateResource404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 404 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("not-found"))
			Expect(problemResp.Title).To(Equal("Resource not found"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(404))
		})

		It("returns 406 when policy rejects rehydration", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 406, Body: "policy rejected rehydration"}
			}

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.RehydrateResource406ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 406 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("policy-rejected"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(406))
		})

		It("returns 409 when new resource ID already exists", func() {
			// Create another resource with a known ID
			existingID := uuid.New().String()
			conflictReq := server.CreateResourceRequestObject{
				Params: apiv1alpha1.CreateResourceParams{Id: &existingID},
				Body: &apiv1alpha1.Resource{
					CatalogItemInstanceId: "catalog-conflict-instance",
					Spec:                  map[string]interface{}{"cpu": 1},
				},
			}
			_, err := handler.CreateResource(ctx, conflictReq)
			Expect(err).NotTo(HaveOccurred())

			// Try to rehydrate with the existing resource's ID as new resource ID
			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: existingID},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.RehydrateResource409ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 409 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("resource-conflict"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(409))
		})

		It("returns 409 when policy conflict detected", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 409, Body: "policy conflict"}
			}

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.RehydrateResource409ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 409 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("resource-conflict"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(409))
		})

		It("returns 422 when SPRM provider error", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 422, Body: "provider error"}
			}

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.RehydrateResource422ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 422 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("provider-error"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(422))
		})

		It("returns 500 when SPRM internal error", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 500, Body: "SPRM internal error"}
			}

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			defaultResp, ok := resp.(server.RehydrateResourcedefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 500 response but got: %T", resp)
			Expect(defaultResp.StatusCode).To(Equal(500))
			Expect(defaultResp.Body.Type).To(Equal("internal-error"))
			Expect(defaultResp.Body.Title).To(Equal("Internal error"))
		})

		It("returns 500 when policy engine internal error", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 500, Body: "policy internal error"}
			}

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			defaultResp, ok := resp.(server.RehydrateResourcedefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 500 response but got: %T", resp)
			Expect(defaultResp.StatusCode).To(Equal(500))
			Expect(defaultResp.Body.Type).To(Equal("internal-error"))
		})

		It("returns 400 when policy validation fails", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 400, Body: "bad request"}
			}

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.RehydrateResource400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 400 response but got: %T", resp)
			Expect(problemResp.Type).To(Equal("validation-error"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(400))
		})

		It("returns 500 when database is closed", func() {
			sqlDB, _ := db.DB()
			_ = sqlDB.Close()

			req := server.RehydrateResourceRequestObject{
				ResourceId: oldResourceID,
				Body:       &apiv1alpha1.RehydrateRequest{NewResourceId: uuid.New().String()},
			}

			resp, err := handler.RehydrateResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			defaultResp, ok := resp.(server.RehydrateResourcedefaultApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "Expected 500 response but got: %T", resp)
			Expect(defaultResp.StatusCode).To(Equal(500))
		})
	})

	Describe("Error Response Structure (RFC 7807)", func() {
		It("returns proper problem+json structure for validation errors (400)", func() {
			// Test validation error with invalid page size
			invalidPageSize := 200
			req := server.ListResourcesRequestObject{
				Params: apiv1alpha1.ListResourcesParams{MaxPageSize: &invalidPageSize},
			}

			resp, err := handler.ListResources(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.ListResources400ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())

			// Verify RFC 7807 required fields
			Expect(problemResp.Type).To(Equal("validation-error"))
			Expect(problemResp.Title).To(Equal("Invalid request"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(400))
			Expect(problemResp.Detail).NotTo(BeNil())
			Expect(*problemResp.Detail).To(ContainSubstring("page size"))
		})

		It("returns proper problem+json structure for not found errors (404)", func() {
			req := server.GetResourceRequestObject{
				ResourceId: uuid.New().String(),
			}

			resp, err := handler.GetResource(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			problemResp, ok := resp.(server.GetResource404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())

			// Verify RFC 7807 required fields
			Expect(problemResp.Type).To(Equal("not-found"))
			Expect(problemResp.Title).To(Equal("Resource not found"))
			Expect(problemResp.Status).NotTo(BeNil())
			Expect(*problemResp.Status).To(Equal(404))
			Expect(problemResp.Detail).NotTo(BeNil())
		})
	})
})
