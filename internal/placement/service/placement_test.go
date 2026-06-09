package service_test

import (
	"context"
	"errors"

	"github.com/dcm-project/control-plane/internal/placement/policy"
	"github.com/dcm-project/control-plane/internal/placement/service"
	"github.com/dcm-project/control-plane/internal/placement/sprm"
	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/dcm-project/control-plane/internal/placement/types"
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
		Status: "provisioning",
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

func getStoredResource(ctx context.Context, dataStore store.Store, id string) *model.Resource {
	r, err := dataStore.Resource().Get(ctx, id)
	Expect(err).NotTo(HaveOccurred())
	return r
}

func expectStoredResourceMissing(ctx context.Context, dataStore store.Store, id string) {
	_, err := dataStore.Resource().Get(ctx, id)
	Expect(err).To(HaveOccurred())
	Expect(errors.Is(err, store.ErrResourceNotFound)).To(BeTrue())
}

var _ = Describe("PlacementService", func() {
	var (
		db           *gorm.DB
		dataStore    store.Store
		mockPolicy   *mockPolicyClient
		mockSPRM     *mockSPRMClient
		placementSvc *service.PlacementService
		ctx          context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Resource{})).To(Succeed())

		dataStore = store.NewStore(db)
		mockPolicy = &mockPolicyClient{}
		mockSPRM = &mockSPRMClient{}
		placementSvc = service.NewPlacementService(dataStore, mockPolicy, mockSPRM)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("CreateResource", func() {
		It("creates resource with APPROVED status from policy", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return &policy.EvaluateResponse{
					Status:           "APPROVED",
					SelectedProvider: "test-provider",
					EvaluatedSpec:    req.Spec,
				}, nil
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-123",
				Spec:                  map[string]any{"cpu": 2, "memory": "4GB"},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Id).NotTo(BeNil())
			Expect(result.CatalogItemInstanceId).To(Equal("catalog-123"))
			Expect(result.Spec).To(HaveKey("cpu"))
			Expect(result.Spec).To(HaveKey("memory"))
			Expect(result.ApprovalStatus).NotTo(BeNil())
			Expect(*result.ApprovalStatus).To(Equal("APPROVED"))
			Expect(result.ProviderName).NotTo(BeNil())
			Expect(*result.ProviderName).To(Equal("test-provider"))

			stored, err := dataStore.Resource().Get(ctx, *result.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.ApprovalStatus).NotTo(BeNil())
			Expect(*stored.ApprovalStatus).To(Equal("APPROVED"))
			Expect(stored.ProviderName).NotTo(BeNil())
			Expect(*stored.ProviderName).To(Equal("test-provider"))
		})

		It("creates resource with MODIFIED status from policy", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				modifiedSpec := make(map[string]any)
				for k, v := range req.Spec {
					modifiedSpec[k] = v
				}
				modifiedSpec["modified_field"] = "policy_value"
				return &policy.EvaluateResponse{
					Status:           "MODIFIED",
					SelectedProvider: "modified-provider",
					EvaluatedSpec:    modifiedSpec,
				}, nil
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-456",
				Spec:                  map[string]any{"cpu": 4},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Spec).To(HaveKey("cpu"))
			Expect(result.Spec).NotTo(HaveKey("modified_field")) // Original spec preserved
			Expect(result.ApprovalStatus).NotTo(BeNil())
			Expect(*result.ApprovalStatus).To(Equal("MODIFIED"))
			Expect(*result.ProviderName).To(Equal("modified-provider"))

			stored, err := dataStore.Resource().Get(ctx, *result.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.ApprovalStatus).NotTo(BeNil())
			Expect(*stored.ApprovalStatus).To(Equal("MODIFIED"))
			Expect(stored.ProviderName).NotTo(BeNil())
			Expect(*stored.ProviderName).To(Equal("modified-provider"))
		})

		It("creates resource with specified ID", func() {
			specifiedID := "custom-resource-id"
			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-789",
				Spec:                  map[string]any{"cpu": 1},
			}

			result, err := placementSvc.CreateResource(ctx, resource, &specifiedID)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(*result.Id).To(Equal(specifiedID))
		})

		It("returns error when policy validation fails (400)", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 400, Body: "bad request"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-invalid",
				Spec:                  map[string]any{"invalid": "spec"},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})

		It("returns error when policy rejects request (406)", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 406, Body: "rejected"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-rejected",
				Spec:                  map[string]any{"cpu": 100},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyRejected))
		})

		It("returns error when policy conflict occurs (409)", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 409, Body: "conflict"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-conflict",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyConflict))
		})

		It("returns error when policy engine fails (500)", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 500, Body: "internal error"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-error",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyInternalError))
		})

		It("returns error when policy returns unmapped HTTP status code", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 418, Body: "I'm a teapot"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-teapot",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyError))
			Expect(svcErr.Message).To(ContainSubstring("policy evaluation failed with status 418"))
			Expect(svcErr.Message).To(ContainSubstring("I'm a teapot"))
		})

		It("returns error when policy response is missing selected provider", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return &policy.EvaluateResponse{
					Status:           "APPROVED",
					SelectedProvider: "",
					EvaluatedSpec:    req.Spec,
				}, nil
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-no-provider",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyInternalError))
			Expect(svcErr.Message).To(ContainSubstring("missing selected provider"))
		})

		It("returns error when policy client communication fails", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, errors.New("connection refused")
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-network-error",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyError))
			Expect(svcErr.Message).To(ContainSubstring("policy client communication error"))
			Expect(svcErr.Message).To(ContainSubstring("connection refused"))
		})

		It("returns conflict error when duplicate ID is used", func() {
			resourceID := "duplicate-id"
			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-dup",
				Spec:                  map[string]any{"cpu": 2},
			}

			// Create first resource
			result1, err := placementSvc.CreateResource(ctx, resource, &resourceID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result1).NotTo(BeNil())

			// Try to create second resource with same ID
			result2, err := placementSvc.CreateResource(ctx, resource, &resourceID)
			Expect(err).To(HaveOccurred())
			Expect(result2).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))
			Expect(svcErr.Message).To(ContainSubstring("already exists"))
		})

		It("returns error and rolls back DB when SPRM creation fails (400)", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 400, Body: "invalid request"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-sprm-400",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))

			// Verify resource was NOT persisted in DB (rollback worked)
			resources, err := dataStore.Resource().List(ctx, &store.ResourceListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resources.Resources).To(BeEmpty())
		})

		It("returns error and rolls back DB when SPRM create fails with canceled request context", func() {
			reqCtx, cancelReq := context.WithCancel(ctx)
			mockSPRM.CreateResourceFunc = func(context.Context, sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				cancelReq()
				return nil, context.Canceled
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-sprm-cancel",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(reqCtx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())

			resources, err := dataStore.Resource().List(ctx, &store.ResourceListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resources.Resources).To(BeEmpty())
		})

		It("returns error and rolls back DB when SPRM creation fails (500)", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 500, Body: "internal error"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-sprm-500",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeSPRMError))

			// Verify resource was NOT persisted in DB (rollback worked)
			resources, err := dataStore.Resource().List(ctx, &store.ResourceListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resources.Resources).To(BeEmpty())
		})

		It("returns provider error when SPRM creation fails (422)", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 422, Body: "provider validation failed"}
			}

			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-sprm-422",
				Spec:                  map[string]any{"cpu": 2},
			}

			result, err := placementSvc.CreateResource(ctx, resource, nil)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeProviderError))

			// Verify resource was NOT persisted in DB (rollback worked)
			resources, err := dataStore.Resource().List(ctx, &store.ResourceListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resources.Resources).To(BeEmpty())
		})
	})

	Describe("DeleteResource", func() {
		It("deletes existing resource", func() {
			// Create a resource first
			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-delete",
				Spec:                  map[string]any{"cpu": 2},
			}
			created, err := placementSvc.CreateResource(ctx, resource, nil)
			Expect(err).NotTo(HaveOccurred())

			// Delete the resource
			err = placementSvc.DeleteResource(ctx, *created.Id)
			Expect(err).NotTo(HaveOccurred())

			// Verify it's deleted
			expectStoredResourceMissing(ctx, dataStore, *created.Id)
		})

		It("returns not found error for non-existent resource", func() {
			err := placementSvc.DeleteResource(ctx, "non-existent-id")

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("returns error when SPRM deletion fails (404)", func() {
			// Create a resource first
			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-sprm-404",
				Spec:                  map[string]any{"cpu": 2},
			}
			created, err := placementSvc.CreateResource(ctx, resource, nil)
			Expect(err).NotTo(HaveOccurred())

			// Mock SPRM delete to fail with 404
			mockSPRM.DeleteResourceFunc = func(_ context.Context, _ string) error {
				return &sprm.HTTPError{StatusCode: 404, Body: "not found in SPRM"}
			}

			// Try to delete the resource
			err = placementSvc.DeleteResource(ctx, *created.Id)

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))

			// Verify resource still exists in DB (SPRM delete failed, so DB delete didn't happen)
			_ = getStoredResource(ctx, dataStore, *created.Id)
		})

		It("returns error when SPRM deletion fails (500)", func() {
			// Create a resource first
			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-sprm-500",
				Spec:                  map[string]any{"cpu": 2},
			}
			created, err := placementSvc.CreateResource(ctx, resource, nil)
			Expect(err).NotTo(HaveOccurred())

			// Mock SPRM delete to fail with 500
			mockSPRM.DeleteResourceFunc = func(_ context.Context, _ string) error {
				return &sprm.HTTPError{StatusCode: 500, Body: "internal error"}
			}

			// Try to delete the resource
			err = placementSvc.DeleteResource(ctx, *created.Id)

			Expect(err).To(HaveOccurred())
			var svcErr *service.ServiceError
			Expect(err).To(BeAssignableToTypeOf(svcErr))
			svcErr = err.(*service.ServiceError)
			Expect(svcErr.Code).To(Equal(service.ErrCodeSPRMError))

			// Verify resource still exists in DB (SPRM delete failed, so DB delete didn't happen)
			_ = getStoredResource(ctx, dataStore, *created.Id)
		})
	})

	Describe("RehydrateResource", func() {
		var (
			oldResourceID string
			catalogID     string
		)

		BeforeEach(func() {
			oldResourceID = "old-resource-id"
			catalogID = "catalog-rehydrate"

			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return &policy.EvaluateResponse{
					Status:           "APPROVED",
					SelectedProvider: "test-provider",
					EvaluatedSpec:    req.Spec,
				}, nil
			}

			resource := &types.Resource{
				CatalogItemInstanceId: catalogID,
				Spec:                  map[string]any{"cpu": 2, "memory": "4GB"},
			}
			_, err := placementSvc.CreateResource(ctx, resource, &oldResourceID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rehydrates a resource successfully", func() {
			newResourceID := "new-resource-id"

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, newResourceID)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(*result.Id).To(Equal(newResourceID))
			Expect(result.CatalogItemInstanceId).To(Equal(catalogID))
			Expect(result.Spec).To(HaveKey("cpu"))
			Expect(result.Spec).To(HaveKey("memory"))
			Expect(*result.ApprovalStatus).To(Equal("APPROVED"))
			Expect(*result.ProviderName).To(Equal("test-provider"))

			// Verify old resource is gone
			expectStoredResourceMissing(ctx, dataStore, oldResourceID)

			// Verify new resource exists
			stored := getStoredResource(ctx, dataStore, newResourceID)
			Expect(stored.CatalogItemInstanceId).To(Equal(catalogID))
		})

		It("re-evaluates policy and assigns new provider", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return &policy.EvaluateResponse{
					Status:           "APPROVED",
					SelectedProvider: "new-provider",
					EvaluatedSpec:    req.Spec,
				}, nil
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-provider")

			Expect(err).NotTo(HaveOccurred())
			Expect(*result.ProviderName).To(Equal("new-provider"))
		})

		It("preserves original spec and sends evaluated spec to SPRM", func() {
			var capturedSPRMSpec map[string]any
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				modifiedSpec := make(map[string]any)
				for k, v := range req.Spec {
					modifiedSpec[k] = v
				}
				modifiedSpec["policy_added"] = "value"
				return &policy.EvaluateResponse{
					Status:           "MODIFIED",
					SelectedProvider: "test-provider",
					EvaluatedSpec:    modifiedSpec,
				}, nil
			}
			mockSPRM.CreateResourceFunc = func(_ context.Context, req sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				capturedSPRMSpec = req.Spec
				return &sprm.CreateResourceResponse{ID: req.ID, Status: "provisioning"}, nil
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-spec")

			Expect(err).NotTo(HaveOccurred())
			Expect(*result.ApprovalStatus).To(Equal("MODIFIED"))
			// DB stores original spec (no policy_added field)
			Expect(result.Spec).NotTo(HaveKey("policy_added"))
			Expect(result.Spec).To(HaveKey("cpu"))
			// SPRM received the evaluated spec (with policy_added field)
			Expect(capturedSPRMSpec).To(HaveKey("policy_added"))
		})

		It("returns not found when old resource does not exist", func() {
			result, err := placementSvc.RehydrateResource(ctx, "non-existent", "new-id")

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})

		It("returns error when policy rejects re-evaluation (406)", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 406, Body: "rejected"}
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-406")

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyRejected))

			// Old resource unchanged
			_ = getStoredResource(ctx, dataStore, oldResourceID)
		})

		It("returns error when policy fails (500)", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, _ policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return nil, &policy.HTTPError{StatusCode: 500, Body: "internal error"}
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-500")

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyInternalError))

			// Old resource unchanged
			_ = getStoredResource(ctx, dataStore, oldResourceID)
		})

		It("returns error when policy returns empty provider", func() {
			mockPolicy.EvaluateFunc = func(_ context.Context, req policy.EvaluateRequest) (*policy.EvaluateResponse, error) {
				return &policy.EvaluateResponse{
					Status:           "APPROVED",
					SelectedProvider: "",
					EvaluatedSpec:    req.Spec,
				}, nil
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-empty")

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodePolicyInternalError))
		})

		It("returns conflict when new resource ID already exists", func() {
			// Create another resource with the ID we want to rehydrate to
			existingID := "existing-id"
			resource := &types.Resource{
				CatalogItemInstanceId: "catalog-existing",
				Spec:                  map[string]any{"cpu": 1},
			}
			_, err := placementSvc.CreateResource(ctx, resource, &existingID)
			Expect(err).NotTo(HaveOccurred())

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, existingID)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))

			// Old resource unchanged
			_ = getStoredResource(ctx, dataStore, oldResourceID)
		})

		It("returns error and rolls back when SPRM creation fails", func() {
			mockSPRM.CreateResourceFunc = func(_ context.Context, _ sprm.CreateResourceRequest) (*sprm.CreateResourceResponse, error) {
				return nil, &sprm.HTTPError{StatusCode: 500, Body: "sprm error"}
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-sprm-fail")

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
			var svcErr *service.ServiceError
			Expect(errors.As(err, &svcErr)).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeSPRMError))

			// Old resource unchanged
			_ = getStoredResource(ctx, dataStore, oldResourceID)

			// New resource was rolled back
			expectStoredResourceMissing(ctx, dataStore, "new-id-sprm-fail")
		})

		It("succeeds even when SPRM deferred delete fails", func() {
			mockSPRM.DeleteResourceDeferredFunc = func(_ context.Context, _ string) error {
				return &sprm.HTTPError{StatusCode: 500, Body: "delete failed"}
			}

			result, err := placementSvc.RehydrateResource(ctx, oldResourceID, "new-id-deferred-fail")

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(*result.Id).To(Equal("new-id-deferred-fail"))

			// New resource exists
			_ = getStoredResource(ctx, dataStore, "new-id-deferred-fail")
		})
	})
})
