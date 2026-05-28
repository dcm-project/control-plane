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
	"github.com/dcm-project/control-plane/internal/catalog/store"
)

// Mock ServiceTypeService for testing
type mockServiceTypeService struct {
	listFunc   func(ctx context.Context, opts *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error)
	createFunc func(ctx context.Context, req *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error)
	getFunc    func(ctx context.Context, id string) (*v1alpha1API.ServiceType, error)
}

func (m *mockServiceTypeService) List(ctx context.Context, opts *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	return &service.ServiceTypeListResult{}, nil
}

func (m *mockServiceTypeService) Create(ctx context.Context, req *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &v1alpha1API.ServiceType{}, nil
}

func (m *mockServiceTypeService) Get(ctx context.Context, id string) (*v1alpha1API.ServiceType, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &v1alpha1API.ServiceType{}, nil
}

// Mock Service
type mockService struct {
	serviceTypeService service.ServiceTypeService
}

func (m *mockService) ServiceType() service.ServiceTypeService {
	return m.serviceTypeService
}

func (m *mockService) CatalogItem() service.CatalogItemService {
	return nil
}

func (m *mockService) CatalogItemInstance() service.CatalogItemInstanceService {
	return nil
}

func (m *mockService) Seed(_ context.Context) error {
	return nil
}

var _ = Describe("ServiceType Handler", func() {
	var (
		ctx           context.Context
		handler       *v1alpha1.Handler
		mockSTService *mockServiceTypeService
		mockSvc       service.Service
		testTime      time.Time
		testID        string
		testPath      string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testTime = time.Now()
		testID = "test-service-type-id"
		testPath = "service-types/" + testID
		mockSTService = &mockServiceTypeService{}
		mockSvc = &mockService{serviceTypeService: mockSTService}
		handler = v1alpha1.NewHandler(mockSvc, slog.Default())
	})

	Describe("CreateServiceType", func() {
		Context("with valid request", func() {
			It("should create a service type and return 201", func() {
				mockSTService.createFunc = func(_ context.Context, req *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
					Expect(req.ServiceType).To(Equal("vm"))
					Expect(req.ApiVersion).To(Equal("v1alpha1"))
					return &v1alpha1API.ServiceType{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": map[string]interface{}{"count": float64(2)}},
						CreateTime:  &testTime,
						UpdateTime:  &testTime,
					}, nil
				}

				request := server.CreateServiceTypeRequestObject{
					Body: &v1alpha1API.ServiceType{
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": map[string]interface{}{"count": 2}},
					},
				}

				response, err := handler.CreateServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateServiceType201JSONResponse{}))

				created := response.(server.CreateServiceType201JSONResponse)
				Expect(*created.Uid).To(Equal(testID))
				Expect(created.ServiceType).To(Equal("vm"))
			})

			It("should create a service type with user-provided ID", func() {
				userID := "my-vm-type"
				mockSTService.createFunc = func(_ context.Context, req *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
					Expect(req.ID).ToNot(BeNil())
					Expect(*req.ID).To(Equal(userID))
					path := "service-types/" + userID
					return &v1alpha1API.ServiceType{
						Uid:         &userID,
						Path:        &path,
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": 2},
						CreateTime:  &testTime,
						UpdateTime:  &testTime,
					}, nil
				}

				request := server.CreateServiceTypeRequestObject{
					Params: v1alpha1API.CreateServiceTypeParams{Id: &userID},
					Body: &v1alpha1API.ServiceType{
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": 2},
					},
				}

				response, err := handler.CreateServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				created := response.(server.CreateServiceType201JSONResponse)
				Expect(*created.Uid).To(Equal(userID))
			})
		})

		Context("with validation errors", func() {
			It("should return 400 for invalid service type", func() {
				mockSTService.createFunc = func(_ context.Context, _ *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
					return nil, service.ErrInvalidServiceType
				}

				request := server.CreateServiceTypeRequestObject{
					Body: &v1alpha1API.ServiceType{
						ApiVersion:  "v1alpha1",
						ServiceType: "invalid",
						Spec:        map[string]interface{}{"foo": "bar"},
					},
				}

				response, err := handler.CreateServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateServiceType400JSONResponse{}))

				badRequest := response.(server.CreateServiceType400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
			})
		})

		Context("with conflict errors", func() {
			It("should return 409 for duplicate ID", func() {
				mockSTService.createFunc = func(_ context.Context, _ *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
					return nil, service.ErrServiceTypeIDTaken
				}

				request := server.CreateServiceTypeRequestObject{
					Body: &v1alpha1API.ServiceType{
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": 2},
					},
				}

				response, err := handler.CreateServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateServiceType409JSONResponse{}))

				conflict := response.(server.CreateServiceType409JSONResponse)
				Expect(conflict.Status).To(Equal(int32(409)))
				Expect(conflict.Type).To(Equal(v1alpha1API.ALREADYEXISTS))
			})

			It("should return 409 for duplicate service type name", func() {
				mockSTService.createFunc = func(_ context.Context, _ *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
					return nil, service.ErrServiceTypeNameTaken
				}

				request := server.CreateServiceTypeRequestObject{
					Body: &v1alpha1API.ServiceType{
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": 2},
					},
				}

				response, err := handler.CreateServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				conflict := response.(server.CreateServiceType409JSONResponse)
				Expect(conflict.Status).To(Equal(int32(409)))
			})
		})

		Context("with unknown errors", func() {
			It("should return 500 for unknown errors", func() {
				mockSTService.createFunc = func(_ context.Context, _ *service.CreateServiceTypeRequest) (*v1alpha1API.ServiceType, error) {
					return nil, errors.New("database connection failed")
				}

				request := server.CreateServiceTypeRequestObject{
					Body: &v1alpha1API.ServiceType{
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]any{"vcpu": 2},
					},
				}

				response, err := handler.CreateServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateServiceType500JSONResponse{}))

				serverError := response.(server.CreateServiceType500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
				Expect(serverError.Type).To(Equal(v1alpha1API.INTERNAL))
			})
		})
	})

	Describe("GetServiceType", func() {
		Context("with valid request", func() {
			It("should retrieve a service type and return 200", func() {
				mockSTService.getFunc = func(_ context.Context, id string) (*v1alpha1API.ServiceType, error) {
					Expect(id).To(Equal(testID))
					return &v1alpha1API.ServiceType{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  "v1alpha1",
						ServiceType: "vm",
						Spec:        map[string]interface{}{"vcpu": 2},
						CreateTime:  &testTime,
						UpdateTime:  &testTime,
					}, nil
				}

				request := server.GetServiceTypeRequestObject{
					ServiceTypeId: testID,
				}

				response, err := handler.GetServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetServiceType200JSONResponse{}))

				retrieved := response.(server.GetServiceType200JSONResponse)
				Expect(*retrieved.Uid).To(Equal(testID))
				Expect(retrieved.ServiceType).To(Equal("vm"))
			})
		})

		Context("with not found error", func() {
			It("should return 404 when service type does not exist", func() {
				mockSTService.getFunc = func(_ context.Context, _ string) (*v1alpha1API.ServiceType, error) {
					return nil, service.ErrServiceTypeNotFound
				}

				request := server.GetServiceTypeRequestObject{
					ServiceTypeId: "non-existent-id",
				}

				response, err := handler.GetServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetServiceType404JSONResponse{}))

				notFound := response.(server.GetServiceType404JSONResponse)
				Expect(notFound.Status).To(Equal(int32(404)))
				Expect(notFound.Type).To(Equal(v1alpha1API.NOTFOUND))
			})
		})

		Context("with unknown errors", func() {
			It("should return 500 for unknown errors", func() {
				mockSTService.getFunc = func(_ context.Context, _ string) (*v1alpha1API.ServiceType, error) {
					return nil, errors.New("database connection failed")
				}

				request := server.GetServiceTypeRequestObject{
					ServiceTypeId: testID,
				}

				response, err := handler.GetServiceType(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetServiceType500JSONResponse{}))

				serverError := response.(server.GetServiceType500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
			})
		})
	})

	Describe("ListServiceTypes", func() {
		Context("with valid request", func() {
			It("should list service types and return 200", func() {
				mockSTService.listFunc = func(_ context.Context, _ *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error) {
					id1 := "vm-type"
					path1 := "service-types/vm-type"
					id2 := "container-type"
					path2 := "service-types/container-type"
					nextPageToken := "token123"
					return &service.ServiceTypeListResult{
						ServiceTypes: []v1alpha1API.ServiceType{
							{
								Uid:         &id1,
								Path:        &path1,
								ApiVersion:  "v1alpha1",
								ServiceType: "vm",
								Spec:        map[string]interface{}{"vcpu": 2},
								CreateTime:  &testTime,
								UpdateTime:  &testTime,
							},
							{
								Uid:         &id2,
								Path:        &path2,
								ApiVersion:  "v1alpha1",
								ServiceType: "container",
								Spec:        map[string]interface{}{"image": "nginx"},
								CreateTime:  &testTime,
								UpdateTime:  &testTime,
							},
						},
						NextPageToken: &nextPageToken,
					}, nil
				}

				request := server.ListServiceTypesRequestObject{}

				response, err := handler.ListServiceTypes(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListServiceTypes200JSONResponse{}))

				list := response.(server.ListServiceTypes200JSONResponse)
				listVal := v1alpha1API.ServiceTypeList(list)
				Expect(listVal.Results).To(HaveLen(2))
				Expect(listVal.NextPageToken).To(Equal("token123"))
				Expect(listVal.Results[0].ServiceType).To(Equal("vm"))
				Expect(listVal.Results[1].ServiceType).To(Equal("container"))
			})

			It("should handle empty list", func() {
				nextPageToken := ""
				mockSTService.listFunc = func(_ context.Context, _ *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error) {
					return &service.ServiceTypeListResult{
						ServiceTypes:  []v1alpha1API.ServiceType{},
						NextPageToken: &nextPageToken,
					}, nil
				}

				request := server.ListServiceTypesRequestObject{}

				response, err := handler.ListServiceTypes(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				list := response.(server.ListServiceTypes200JSONResponse)
				listVal := v1alpha1API.ServiceTypeList(list)
				Expect(listVal.Results).To(BeEmpty())
				Expect(listVal.NextPageToken).To(BeEmpty())
			})

			It("should pass page token to service", func() {
				token := "page-token-123"
				mockSTService.listFunc = func(_ context.Context, opts *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error) {
					Expect(opts.PageToken).ToNot(BeNil())
					Expect(*opts.PageToken).To(Equal(token))
					nextPageToken := ""
					return &service.ServiceTypeListResult{
						ServiceTypes:  []v1alpha1API.ServiceType{},
						NextPageToken: &nextPageToken,
					}, nil
				}

				request := server.ListServiceTypesRequestObject{
					Params: v1alpha1API.ListServiceTypesParams{PageToken: &token},
				}

				response, err := handler.ListServiceTypes(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListServiceTypes200JSONResponse{}))
			})
		})

		Context("with errors", func() {
			It("should return 400 for invalid page_token", func() {
				mockSTService.listFunc = func(_ context.Context, _ *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error) {
					return nil, store.ErrInvalidPageToken
				}

				request := server.ListServiceTypesRequestObject{}

				response, err := handler.ListServiceTypes(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListServiceTypes400JSONResponse{}))

				badRequest := response.(server.ListServiceTypes400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
			})

			It("should return 500 for unknown errors", func() {
				mockSTService.listFunc = func(_ context.Context, _ *service.ServiceTypeListOptions) (*service.ServiceTypeListResult, error) {
					return nil, errors.New("database connection failed")
				}

				request := server.ListServiceTypesRequestObject{}

				response, err := handler.ListServiceTypes(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListServiceTypes500JSONResponse{}))

				serverError := response.(server.ListServiceTypes500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
			})
		})
	})
})
