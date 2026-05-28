package v1alpha1_test

import (
	"context"
	"errors"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1API "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	v1alpha1 "github.com/dcm-project/control-plane/internal/catalog/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

// Mock CatalogItemService for testing
type mockCatalogItemService struct {
	listFunc   func(ctx context.Context, opts service.CatalogItemListOptions) (*service.CatalogItemListResult, error)
	createFunc func(ctx context.Context, req *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error)
	getFunc    func(ctx context.Context, id string) (*v1alpha1API.CatalogItem, error)
	updateFunc func(ctx context.Context, id string, req *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error)
	deleteFunc func(ctx context.Context, id string) error
}

func (m *mockCatalogItemService) List(ctx context.Context, opts service.CatalogItemListOptions) (*service.CatalogItemListResult, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	return &service.CatalogItemListResult{}, nil
}

func (m *mockCatalogItemService) Create(ctx context.Context, req *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &v1alpha1API.CatalogItem{}, nil
}

func (m *mockCatalogItemService) Get(ctx context.Context, id string) (*v1alpha1API.CatalogItem, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &v1alpha1API.CatalogItem{}, nil
}

func (m *mockCatalogItemService) Update(ctx context.Context, id string, req *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, req)
	}
	return &v1alpha1API.CatalogItem{}, nil
}

func (m *mockCatalogItemService) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

// Mock Service with CatalogItem
type mockCatalogItemServiceWrapper struct {
	catalogItemService service.CatalogItemService
}

func (m *mockCatalogItemServiceWrapper) ServiceType() service.ServiceTypeService {
	return nil
}

func (m *mockCatalogItemServiceWrapper) CatalogItem() service.CatalogItemService {
	return m.catalogItemService
}

func (m *mockCatalogItemServiceWrapper) CatalogItemInstance() service.CatalogItemInstanceService {
	return nil
}

func (m *mockCatalogItemServiceWrapper) Seed(_ context.Context) error {
	return nil
}

var _ = Describe("CatalogItem Handler", func() {
	var (
		ctx                  context.Context
		handler              *v1alpha1.Handler
		mockCIService        *mockCatalogItemService
		mockSvc              service.Service
		testTime             time.Time
		testID               string
		testPath             string
		testApiVersion       = "v1alpha1"
		serviceTypeVM        = "vm"
		serviceTypeContainer = "container"
		strintPtr            = func(s string) *string { return &s }
	)

	BeforeEach(func() {
		ctx = context.Background()
		testTime = time.Now()
		testID = "test-catalog-item-id"
		testPath = "catalog-items/" + testID
		mockCIService = &mockCatalogItemService{}
		mockSvc = &mockCatalogItemServiceWrapper{catalogItemService: mockCIService}
		handler = v1alpha1.NewHandler(mockSvc, slog.Default())
	})

	Describe("CreateCatalogItem", func() {
		Context("with valid request", func() {
			It("should create a catalog item and return 201", func() {
				displayName := "Test Catalog Item"
				mockCIService.createFunc = func(_ context.Context, req *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					Expect(req.DisplayName).To(Equal(displayName))
					Expect(req.ApiVersion).To(Equal("v1alpha1"))
					Expect(*req.Spec.ServiceType).To(Equal(serviceTypeVM))
					return &v1alpha1API.CatalogItem{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr(displayName),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields: &[]v1alpha1API.FieldConfiguration{
								{Path: "spec.vcpu.count", Default: 2},
							},
						},
						CreateTime: &testTime,
						UpdateTime: &testTime,
					}, nil
				}

				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr(displayName),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields: &[]v1alpha1API.FieldConfiguration{
								{Path: "spec.vcpu.count", Default: 2},
							},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem201JSONResponse{}))

				created := response.(server.CreateCatalogItem201JSONResponse)
				Expect(*created.Uid).To(Equal(testID))
				Expect(*created.DisplayName).To(Equal(displayName))
			})

			It("should handle optional ID query param", func() {
				userID := "my-catalog-item"
				displayName := "My Item"
				mockCIService.createFunc = func(_ context.Context, req *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					Expect(req.ID).ToNot(BeNil())
					Expect(*req.ID).To(Equal(userID))
					path := "catalog-items/" + userID
					return &v1alpha1API.CatalogItem{
						Uid:         &userID,
						Path:        &path,
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr(displayName),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
						CreateTime: &testTime,
						UpdateTime: &testTime,
					}, nil
				}

				request := server.CreateCatalogItemRequestObject{
					Params: v1alpha1API.CreateCatalogItemParams{Id: &userID},
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr(displayName),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				created := response.(server.CreateCatalogItem201JSONResponse)
				Expect(*created.Uid).To(Equal(userID))
			})
		})

		Context("with validation errors", func() {
			It("should return 400 when api_version is nil", func() {
				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  nil,
						DisplayName: strintPtr("My Item"),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("api_version"))
			})

			It("should return 400 when api_version is not v1alpha1", func() {
				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  strintPtr("v1beta1"),
						DisplayName: strintPtr("My Item"),
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("api_version"))
			})

			It("should return 400 when display_name is nil", func() {
				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: nil,
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("display_name"))
			})

			It("should return 400 when spec is nil", func() {
				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("My Item"),
						Spec:        nil,
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("spec"))
			})

			It("should return 400 when spec.service_type is nil", func() {
				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("My Item"),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: nil,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("spec.service_type"))
			})

			It("should return 400 when spec.fields is nil", func() {
				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("My Item"),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      nil,
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("fields"))
			})
		})

		Context("with duplicate ID", func() {
			It("should return 409 conflict", func() {
				mockCIService.createFunc = func(_ context.Context, _ *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					return nil, service.ErrCatalogItemIDTaken
				}

				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("Duplicate"),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem409JSONResponse{}))

				conflict := response.(server.CreateCatalogItem409JSONResponse)
				Expect(conflict.Status).To(Equal(int32(409)))
				Expect(conflict.Type).To(Equal(v1alpha1API.ALREADYEXISTS))
			})
		})

		Context("with service type not found", func() {
			It("should return 400 bad request", func() {
				mockCIService.createFunc = func(_ context.Context, _ *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					return nil, service.ErrServiceTypeNotFound
				}

				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("Test"),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(badRequest.Detail).ToNot(BeNil())
				Expect(*badRequest.Detail).To(ContainSubstring("service type not found"))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIService.createFunc = func(_ context.Context, _ *service.CreateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					return nil, errors.New("database error")
				}

				request := server.CreateCatalogItemRequestObject{
					Body: &v1alpha1API.CatalogItem{
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("Test"),
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1API.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					},
				}

				response, err := handler.CreateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItem500JSONResponse{}))

				serverError := response.(server.CreateCatalogItem500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
				Expect(serverError.Type).To(Equal(v1alpha1API.INTERNAL))
			})
		})
	})

	Describe("ListCatalogItems", func() {
		Context("with valid request", func() {
			It("should list catalog items and return 200", func() {
				mockCIService.listFunc = func(_ context.Context, _ service.CatalogItemListOptions) (*service.CatalogItemListResult, error) {
					return &service.CatalogItemListResult{
						CatalogItems: []v1alpha1API.CatalogItem{
							{
								Uid:         &testID,
								Path:        &testPath,
								ApiVersion:  &testApiVersion,
								DisplayName: strintPtr("Item 1"),
								Spec:        &v1alpha1API.CatalogItemSpec{ServiceType: &serviceTypeVM},
							},
						},
					}, nil
				}

				request := server.ListCatalogItemsRequestObject{}

				response, err := handler.ListCatalogItems(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListCatalogItems200JSONResponse{}))

				list := response.(server.ListCatalogItems200JSONResponse)
				Expect(list.Results).To(HaveLen(1))
			})

			It("should pass pagination params correctly", func() {
				pageToken := "token123"
				pageSize := int32(10)
				nextToken := "token123"
				mockCIService.listFunc = func(_ context.Context, opts service.CatalogItemListOptions) (*service.CatalogItemListResult, error) {
					Expect(opts.PageToken).To(Equal(&pageToken))
					Expect(opts.MaxPageSize).To(Equal(&pageSize))
					return &service.CatalogItemListResult{
						CatalogItems:  []v1alpha1API.CatalogItem{},
						NextPageToken: &nextToken,
					}, nil
				}

				request := server.ListCatalogItemsRequestObject{
					Params: v1alpha1API.ListCatalogItemsParams{
						PageToken:   &pageToken,
						MaxPageSize: &pageSize,
					},
				}

				response, err := handler.ListCatalogItems(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				list := response.(server.ListCatalogItems200JSONResponse)
				Expect(list.NextPageToken).To(Equal(nextToken))
			})

			It("should pass service type filter correctly", func() {
				mockCIService.listFunc = func(_ context.Context, opts service.CatalogItemListOptions) (*service.CatalogItemListResult, error) {
					Expect(opts.ServiceType).To(Equal(&serviceTypeVM))
					return &service.CatalogItemListResult{
						CatalogItems: []v1alpha1API.CatalogItem{},
					}, nil
				}

				request := server.ListCatalogItemsRequestObject{
					Params: v1alpha1API.ListCatalogItemsParams{
						ServiceType: &serviceTypeVM,
					},
				}

				response, err := handler.ListCatalogItems(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListCatalogItems200JSONResponse{}))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIService.listFunc = func(_ context.Context, _ service.CatalogItemListOptions) (*service.CatalogItemListResult, error) {
					return nil, errors.New("database error")
				}

				request := server.ListCatalogItemsRequestObject{}

				response, err := handler.ListCatalogItems(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListCatalogItems500JSONResponse{}))

				serverError := response.(server.ListCatalogItems500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
			})
		})
	})

	Describe("GetCatalogItem", func() {
		Context("with valid request", func() {
			It("should get a catalog item and return 200", func() {
				mockCIService.getFunc = func(_ context.Context, id string) (*v1alpha1API.CatalogItem, error) {
					Expect(id).To(Equal(testID))
					return &v1alpha1API.CatalogItem{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr("Test Item"),
						Spec:        &v1alpha1API.CatalogItemSpec{ServiceType: &serviceTypeVM},
						CreateTime:  &testTime,
						UpdateTime:  &testTime,
					}, nil
				}

				request := server.GetCatalogItemRequestObject{
					CatalogItemId: testID,
				}

				response, err := handler.GetCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetCatalogItem200JSONResponse{}))

				item := response.(server.GetCatalogItem200JSONResponse)
				Expect(*item.Uid).To(Equal(testID))
			})
		})

		Context("with not found error", func() {
			It("should return 404 not found", func() {
				mockCIService.getFunc = func(_ context.Context, _ string) (*v1alpha1API.CatalogItem, error) {
					return nil, service.ErrCatalogItemNotFound
				}

				request := server.GetCatalogItemRequestObject{
					CatalogItemId: "nonexistent",
				}

				response, err := handler.GetCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetCatalogItem404JSONResponse{}))

				notFound := response.(server.GetCatalogItem404JSONResponse)
				Expect(notFound.Status).To(Equal(int32(404)))
				Expect(notFound.Type).To(Equal(v1alpha1API.NOTFOUND))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIService.getFunc = func(_ context.Context, _ string) (*v1alpha1API.CatalogItem, error) {
					return nil, errors.New("database error")
				}

				request := server.GetCatalogItemRequestObject{
					CatalogItemId: testID,
				}

				response, err := handler.GetCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetCatalogItem500JSONResponse{}))

				serverError := response.(server.GetCatalogItem500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
				Expect(serverError.Type).To(Equal(v1alpha1API.INTERNAL))
			})
		})
	})

	Describe("UpdateCatalogItem", func() {
		Context("with valid update", func() {
			It("should update catalog item and return 200", func() {
				displayName := "Updated Name"
				mockCIService.updateFunc = func(_ context.Context, id string, req *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					Expect(id).To(Equal(testID))
					Expect(req.DisplayName).ToNot(BeNil())
					Expect(*req.DisplayName).To(Equal(displayName))
					return &v1alpha1API.CatalogItem{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  &testApiVersion,
						DisplayName: strintPtr(displayName),
						Spec:        &v1alpha1API.CatalogItemSpec{ServiceType: &serviceTypeVM},
						UpdateTime:  &testTime,
					}, nil
				}

				request := server.UpdateCatalogItemRequestObject{
					CatalogItemId: testID,
					Body: &v1alpha1API.CatalogItem{
						DisplayName: strintPtr(displayName),
					},
				}

				response, err := handler.UpdateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.UpdateCatalogItem200JSONResponse{}))

				updated := response.(server.UpdateCatalogItem200JSONResponse)
				Expect(*updated.DisplayName).To(Equal(displayName))
			})

			It("should update display_name only", func() {
				displayName := "New Name"
				mockCIService.updateFunc = func(_ context.Context, _ string, req *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					Expect(req.DisplayName).ToNot(BeNil())
					Expect(req.Spec).To(BeNil())
					return &v1alpha1API.CatalogItem{
						Uid:         &testID,
						DisplayName: strintPtr(displayName),
						Spec:        &v1alpha1API.CatalogItemSpec{ServiceType: &serviceTypeVM},
					}, nil
				}

				request := server.UpdateCatalogItemRequestObject{
					CatalogItemId: testID,
					Body: &v1alpha1API.CatalogItem{
						DisplayName: strintPtr(displayName),
					},
				}

				response, err := handler.UpdateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.UpdateCatalogItem200JSONResponse{}))
			})
		})

		Context("with immutable field update attempt", func() {
			It("should return 400 for immutable field", func() {
				mockCIService.updateFunc = func(_ context.Context, _ string, _ *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					return nil, service.ErrImmutableFieldUpdate
				}

				request := server.UpdateCatalogItemRequestObject{
					CatalogItemId: testID,
					Body: &v1alpha1API.CatalogItem{
						ApiVersion: strintPtr("v2beta1"), // Attempting to change immutable field
						Spec: &v1alpha1API.CatalogItemSpec{
							ServiceType: &serviceTypeContainer, // Attempting to change immutable field
						},
					},
				}

				response, err := handler.UpdateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.UpdateCatalogItem400JSONResponse{}))

				badRequest := response.(server.UpdateCatalogItem400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
			})
		})

		Context("with not found error", func() {
			It("should return 404 not found", func() {
				mockCIService.updateFunc = func(_ context.Context, _ string, _ *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					return nil, service.ErrCatalogItemNotFound
				}

				request := server.UpdateCatalogItemRequestObject{
					CatalogItemId: "nonexistent",
					Body: &v1alpha1API.CatalogItem{
						DisplayName: strintPtr("Updated"),
					},
				}

				response, err := handler.UpdateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.UpdateCatalogItem404JSONResponse{}))

				notFound := response.(server.UpdateCatalogItem404JSONResponse)
				Expect(notFound.Status).To(Equal(int32(404)))
				Expect(notFound.Type).To(Equal(v1alpha1API.NOTFOUND))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIService.updateFunc = func(_ context.Context, _ string, _ *service.UpdateCatalogItemRequest) (*v1alpha1API.CatalogItem, error) {
					return nil, errors.New("database error")
				}

				request := server.UpdateCatalogItemRequestObject{
					CatalogItemId: testID,
					Body: &v1alpha1API.CatalogItem{
						DisplayName: strintPtr("Updated"),
					},
				}

				response, err := handler.UpdateCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.UpdateCatalogItem500JSONResponse{}))

				serverError := response.(server.UpdateCatalogItem500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
				Expect(serverError.Type).To(Equal(v1alpha1API.INTERNAL))
			})
		})
	})

	Describe("DeleteCatalogItem", func() {
		Context("with valid request", func() {
			It("should delete catalog item and return 204", func() {
				mockCIService.deleteFunc = func(_ context.Context, id string) error {
					Expect(id).To(Equal(testID))
					return nil
				}

				request := server.DeleteCatalogItemRequestObject{
					CatalogItemId: testID,
				}

				response, err := handler.DeleteCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItem204Response{}))
			})
		})

		Context("with not found error", func() {
			It("should return 404 not found", func() {
				mockCIService.deleteFunc = func(_ context.Context, _ string) error {
					return service.ErrCatalogItemNotFound
				}

				request := server.DeleteCatalogItemRequestObject{
					CatalogItemId: "nonexistent",
				}

				response, err := handler.DeleteCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItem404JSONResponse{}))

				notFound := response.(server.DeleteCatalogItem404JSONResponse)
				Expect(notFound.Status).To(Equal(int32(404)))
				Expect(notFound.Type).To(Equal(v1alpha1API.NOTFOUND))
			})
		})

		Context("with catalog item has instances", func() {
			It("should return 409 conflict", func() {
				mockCIService.deleteFunc = func(_ context.Context, _ string) error {
					return service.ErrCatalogItemHasInstances
				}

				request := server.DeleteCatalogItemRequestObject{
					CatalogItemId: testID,
				}

				response, err := handler.DeleteCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItem409JSONResponse{}))

				conflict := response.(server.DeleteCatalogItem409JSONResponse)
				Expect(conflict.Status).To(Equal(int32(409)))
				Expect(conflict.Type).To(Equal(v1alpha1API.FAILEDPRECONDITION))
				Expect(*conflict.Detail).To(ContainSubstring("catalog item has existing instances"))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIService.deleteFunc = func(_ context.Context, _ string) error {
					return errors.New("database error")
				}

				request := server.DeleteCatalogItemRequestObject{
					CatalogItemId: testID,
				}

				response, err := handler.DeleteCatalogItem(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItem500JSONResponse{}))

				serverError := response.(server.DeleteCatalogItem500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
				Expect(serverError.Type).To(Equal(v1alpha1API.INTERNAL))
			})
		})
	})
})
