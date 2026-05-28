package service_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	"github.com/dcm-project/control-plane/internal/catalog/placement"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

// mockPMClient is a mock Placement Manager client for testing
type mockPMClient struct {
	createFunc     func(ctx context.Context, req placement.CreateResourceRequest, id string) (*placement.Resource, error)
	deleteFunc     func(ctx context.Context, resourceID string) error
	rehydrateFunc  func(ctx context.Context, resourceID string, newResourceID string) (*placement.Resource, error)
	createCalls    int
	deleteCalls    int
	rehydrateCalls int
}

func (m *mockPMClient) CreateResource(ctx context.Context, req placement.CreateResourceRequest, id string) (*placement.Resource, error) {
	m.createCalls++
	if m.createFunc != nil {
		return m.createFunc(ctx, req, id)
	}
	return &placement.Resource{ID: "pm-" + id}, nil
}

func (m *mockPMClient) DeleteResource(ctx context.Context, resourceID string) error {
	m.deleteCalls++
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, resourceID)
	}
	return nil
}

func (m *mockPMClient) RehydrateResource(ctx context.Context, resourceID string, newResourceID string) (*placement.Resource, error) {
	m.rehydrateCalls++
	if m.rehydrateFunc != nil {
		return m.rehydrateFunc(ctx, resourceID, newResourceID)
	}
	return &placement.Resource{ID: newResourceID}, nil
}

func ensureCatalogItem(ctx context.Context, str store.Store, id, serviceType string) {
	ci := model.CatalogItem{
		ID:          id,
		ApiVersion:  "v1alpha1",
		DisplayName: fmt.Sprintf("Test %s", id),
		Spec: model.CatalogItemSpec{
			ServiceType: serviceType,
			Fields:      []model.FieldConfiguration{},
		},
		Path:            fmt.Sprintf("catalog-items/%s", id),
		SpecServiceType: serviceType,
	}
	_, err := str.CatalogItem().Create(ctx, ci)
	if err != nil {
		return
	}
}

func ensureCatalogItemWithFields(ctx context.Context, str store.Store, id, serviceType string, fields []model.FieldConfiguration) {
	ci := model.CatalogItem{
		ID:          id,
		ApiVersion:  "v1alpha1",
		DisplayName: fmt.Sprintf("Test %s", id),
		Spec: model.CatalogItemSpec{
			ServiceType: serviceType,
			Fields:      fields,
		},
		Path:            fmt.Sprintf("catalog-items/%s", id),
		SpecServiceType: serviceType,
	}
	_, err := str.CatalogItem().Create(ctx, ci)
	if err != nil {
		return
	}
}

func ensureServiceTypeWithSpec(ctx context.Context, str store.Store, id, serviceType string, spec map[string]any) {
	st := model.ServiceType{
		ID:          id,
		ApiVersion:  "v1alpha1",
		ServiceType: serviceType,
		Spec:        spec,
		Path:        fmt.Sprintf("service-types/%s", id),
	}
	_, err := str.ServiceType().Create(ctx, st)
	if err != nil {
		return
	}
}

var _ = Describe("CatalogItemInstance Service", func() {
	var (
		ctx    context.Context
		db     *gorm.DB
		str    store.Store
		svc    service.Service
		mockPM *mockPMClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())
		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		mockPM = &mockPMClient{}
		svc, err = service.NewService(str, mockPM, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
		// Ensure prerequisites with specs
		ensureServiceTypeWithSpec(ctx, str, "vm-st", "vm", map[string]any{
			"vcpu":   map[string]any{"count": float64(2)},
			"memory": map[string]any{"size_gb": float64(4)},
		})
		ensureServiceTypeWithSpec(ctx, str, "container-st", "container", map[string]any{
			"image":    "nginx",
			"replicas": float64(1),
		})
		ensureCatalogItem(ctx, str, "small-vm", "vm")
		ensureCatalogItem(ctx, str, "small-container", "container")
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("Create", func() {
		Context("with valid user-provided ID", func() {
			It("should create a catalog item instance with the provided ID", func() {
				userID := "my-instance"
				req := &service.CreateCatalogItemInstanceRequest{
					ID:          &userID,
					ApiVersion:  "v1alpha1",
					DisplayName: "My VM Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Uid).To(Equal(userID))
				Expect(result.DisplayName).To(Equal("My VM Instance"))
				Expect(result.Spec.CatalogItemId).To(Equal("small-vm"))
				Expect(*result.Path).To(Equal("catalog-item-instances/my-instance"))
				Expect(result.ResourceId).ToNot(BeNil())
				Expect(*result.ResourceId).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
			})
		})

		Context("without ID (auto-generate UUID)", func() {
			It("should auto-generate a UUID for the instance", func() {
				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Auto ID Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Uid).ToNot(BeNil())
				Expect(*result.Uid).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
				Expect(result.ResourceId).ToNot(BeNil())
				Expect(*result.ResourceId).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
				Expect(*result.ResourceId).ToNot(Equal(*result.Uid))
			})
		})

		Context("when store returns duplicate ID error", func() {
			It("should return ErrCatalogItemInstanceIDTaken", func() {
				id := "taken-id"
				req1 := &service.CreateCatalogItemInstanceRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "First",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				}
				_, err := svc.CatalogItemInstance().Create(ctx, req1)
				Expect(err).ToNot(HaveOccurred())

				req2 := &service.CreateCatalogItemInstanceRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Second",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				}
				result, err := svc.CatalogItemInstance().Create(ctx, req2)
				Expect(err).To(Equal(service.ErrCatalogItemInstanceIDTaken))
				Expect(result).To(BeNil())
				// Make sure create was called only once (for the first request)
				Expect(mockPM.createCalls).To(Equal(1))
				// Make sure delete was not called (since the second request fast-failed)
				Expect(mockPM.deleteCalls).To(Equal(0))
			})
		})

		Context("when catalog_item_id does not exist", func() {
			It("should return ErrCatalogItemNotFoundForInstance", func() {
				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Bad Reference",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "nonexistent-catalog-item",
						UserValues:    []v1alpha1.UserValue{},
					},
				}
				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).To(Equal(service.ErrCatalogItemNotFoundForInstance))
				Expect(result).To(BeNil())
				Expect(mockPM.createCalls).To(Equal(0))
				Expect(mockPM.deleteCalls).To(Equal(0))
			})
		})

		Context("with spec validation", func() {
			It("should apply user_values for editable fields", func() {
				ensureCatalogItemWithFields(ctx, str, "vm-with-fields", "vm", []model.FieldConfiguration{
					{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
					{Path: "spec.memory.size_gb", Default: float64(4), Editable: false},
				})

				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "VM with overrides",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "vm-with-fields",
						UserValues: []v1alpha1.UserValue{
							{Path: "spec.vcpu.count", Value: float64(8)},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should reject user_value for non-existent field path", func() {
				ensureCatalogItemWithFields(ctx, str, "vm-no-disk", "vm", []model.FieldConfiguration{
					{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
				})

				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Bad path",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "vm-no-disk",
						UserValues: []v1alpha1.UserValue{
							{Path: "spec.disk.size", Value: float64(100)},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("user value path not found"))
				Expect(result).To(BeNil())
				Expect(mockPM.createCalls).To(Equal(0))
				Expect(mockPM.deleteCalls).To(Equal(0))
			})

			It("should reject user_value for non-editable field", func() {
				ensureCatalogItemWithFields(ctx, str, "vm-immutable", "vm", []model.FieldConfiguration{
					{Path: "spec.memory.size_gb", Default: float64(4), Editable: false},
				})

				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Non-editable",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "vm-immutable",
						UserValues: []v1alpha1.UserValue{
							{Path: "spec.memory.size_gb", Value: float64(16)},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not editable"))
				Expect(result).To(BeNil())
				Expect(mockPM.createCalls).To(Equal(0))
				Expect(mockPM.deleteCalls).To(Equal(0))
			})

			It("should reject user_value that fails validation_schema", func() {
				ensureCatalogItemWithFields(ctx, str, "vm-validated", "vm", []model.FieldConfiguration{
					{
						Path:     "spec.vcpu.count",
						Default:  float64(2),
						Editable: true,
						ValidationSchema: map[string]any{
							"type":    "number",
							"minimum": float64(1),
							"maximum": float64(16),
						},
					},
				})

				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Bad value",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "vm-validated",
						UserValues: []v1alpha1.UserValue{
							{Path: "spec.vcpu.count", Value: float64(32)},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("validation failed"))
				Expect(result).To(BeNil())
				Expect(mockPM.createCalls).To(Equal(0))
				Expect(mockPM.deleteCalls).To(Equal(0))
			})

			It("should accept user_value that passes validation_schema", func() {
				ensureCatalogItemWithFields(ctx, str, "vm-valid-schema", "vm", []model.FieldConfiguration{
					{
						Path:     "spec.vcpu.count",
						Default:  float64(2),
						Editable: true,
						ValidationSchema: map[string]any{
							"type":    "number",
							"minimum": float64(1),
							"maximum": float64(16),
						},
					},
				})

				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Valid value",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "vm-valid-schema",
						UserValues: []v1alpha1.UserValue{
							{Path: "spec.vcpu.count", Value: float64(8)},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should succeed with defaults only (no user_values)", func() {
				ensureCatalogItemWithFields(ctx, str, "vm-defaults-only", "vm", []model.FieldConfiguration{
					{Path: "spec.vcpu.count", Default: float64(2), Editable: true},
					{Path: "spec.memory.size_gb", Default: float64(4), Editable: false},
				})

				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Defaults only",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "vm-defaults-only",
						UserValues:    []v1alpha1.UserValue{},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})
	})

	Describe("List", func() {
		Context("without filters", func() {
			It("should return all catalog item instances", func() {
				_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Instance 1",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Instance 2",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-container",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				result, err := svc.CatalogItemInstance().List(ctx, service.CatalogItemInstanceListOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItemInstances).To(HaveLen(2))
			})
		})

		Context("with catalog_item_id filter", func() {
			It("should filter by catalog_item_id", func() {
				_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "VM Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Container Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-container",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				ciFilter := "small-vm"
				result, err := svc.CatalogItemInstance().List(ctx, service.CatalogItemInstanceListOptions{CatalogItemId: &ciFilter})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItemInstances).To(HaveLen(1))
				Expect(result.CatalogItemInstances[0].Spec.CatalogItemId).To(Equal("small-vm"))
			})
		})

		Context("with pagination options", func() {
			It("should pass pagination parameters and return next page token when more results exist", func() {
				for i := range 6 {
					_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
						ApiVersion:  "v1alpha1",
						DisplayName: fmt.Sprintf("Instance %d", i),
						Spec: v1alpha1.CatalogItemInstanceSpec{
							CatalogItemId: "small-vm",
							UserValues:    []v1alpha1.UserValue{},
						},
					})
					Expect(err).ToNot(HaveOccurred())
				}

				maxPageSize := int32(2)
				result, err := svc.CatalogItemInstance().List(ctx, service.CatalogItemInstanceListOptions{
					MaxPageSize: &maxPageSize,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItemInstances).To(HaveLen(2))
				Expect(result.NextPageToken).ToNot(BeNil())

				maxPageSize = int32(3)
				result, err = svc.CatalogItemInstance().List(ctx, service.CatalogItemInstanceListOptions{
					MaxPageSize: &maxPageSize,
					PageToken:   result.NextPageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItemInstances).To(HaveLen(3))
				Expect(result.NextPageToken).ToNot(BeNil())

				maxPageSize = int32(4)
				result, err = svc.CatalogItemInstance().List(ctx, service.CatalogItemInstanceListOptions{
					MaxPageSize: &maxPageSize,
					PageToken:   result.NextPageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItemInstances).To(HaveLen(1))
				Expect(result.NextPageToken).To(BeNil())
			})
		})
	})

	Describe("Get", func() {
		Context("with valid ID", func() {
			It("should return the catalog item instance", func() {
				created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Test Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(created.Uid).ToNot(BeNil())

				result, err := svc.CatalogItemInstance().Get(ctx, *created.Uid)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Uid).To(Equal(*created.Uid))
				Expect(result.DisplayName).To(Equal("Test Instance"))
				Expect(result.ResourceId).ToNot(BeNil())
				Expect(*result.ResourceId).To(Equal(*created.ResourceId))
			})
		})

		Context("with non-existent ID", func() {
			It("should return ErrCatalogItemInstanceNotFound", func() {
				result, err := svc.CatalogItemInstance().Get(ctx, "nonexistent")
				Expect(err).To(Equal(service.ErrCatalogItemInstanceNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Delete", func() {
		Context("with existing instance", func() {
			It("should delete the catalog item instance", func() {
				id := "to-delete"
				_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "To Delete",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "small-vm",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				err = svc.CatalogItemInstance().Delete(ctx, "to-delete")
				Expect(err).ToNot(HaveOccurred())

				_, err = svc.CatalogItemInstance().Get(ctx, "to-delete")
				Expect(err).To(Equal(service.ErrCatalogItemInstanceNotFound))
			})
		})

		Context("with non-existent instance", func() {
			It("should return ErrCatalogItemInstanceNotFound", func() {
				err := svc.CatalogItemInstance().Delete(ctx, "nonexistent")
				Expect(err).To(Equal(service.ErrCatalogItemInstanceNotFound))
			})
		})
	})
})

var _ = Describe("CatalogItemInstance Service with Placement Manager", func() {
	var (
		ctx    context.Context
		db     *gorm.DB
		str    store.Store
		svc    service.Service
		mockPM *mockPMClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())
		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		mockPM = &mockPMClient{}
		svc, err = service.NewService(str, mockPM, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
		// Ensure prerequisites
		ensureServiceTypeWithSpec(ctx, str, "vm-st", "vm", map[string]any{
			"vcpu":   map[string]any{"count": float64(2)},
			"memory": map[string]any{"size_gb": float64(4)},
		})
		ensureCatalogItem(ctx, str, "small-vm", "vm")
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("Create with PM", func() {
		It("should call PM with separate resource ID and store it", func() {
			var capturedReq placement.CreateResourceRequest
			var capturedID string
			mockPM.createFunc = func(_ context.Context, req placement.CreateResourceRequest, id string) (*placement.Resource, error) {
				capturedReq = req
				capturedID = id
				return &placement.Resource{ID: id}, nil
			}

			instanceID := "my-pm-instance"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Test Instance",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(capturedReq.CatalogItemInstanceID).To(Equal(instanceID))
			// Resource ID passed to PM should be a UUID, different from instance ID
			Expect(capturedID).ToNot(Equal(instanceID))
			Expect(capturedID).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
			Expect(capturedReq.Spec).ToNot(BeNil())
			// Resource ID should be stored and returned in the API response
			Expect(result.ResourceId).ToNot(BeNil())
			Expect(*result.ResourceId).To(Equal(capturedID))

			// Verify the resource ID is stored and returned in the API response
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).ToNot(BeNil())
			Expect(*got.ResourceId).To(Equal(capturedID))
		})

		It("should delete DB record when PM create fails with canceled request context", func() {
			reqCtx, cancelReq := context.WithCancel(ctx)
			mockPM.createFunc = func(context.Context, placement.CreateResourceRequest, string) (*placement.Resource, error) {
				cancelReq()
				return nil, context.Canceled
			}

			instanceID := "pm-ctx-cancel-rollback"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Context Cancel",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(reqCtx, req)
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())

			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})

		It("should delete DB record when PM create fails", func() {
			mockPM.createFunc = func(_ context.Context, _ placement.CreateResourceRequest, _ string) (*placement.Resource, error) {
				return nil, errors.New("PM unavailable")
			}

			instanceID := "pm-fail-instance"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Fail Test",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("placement manager create resource failed"))
			Expect(result).To(BeNil())

			// Verify DB record was cleaned up (rollback)
			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})

		It("should return ErrPlacementManagerPolicyRejected when PM create returns 406", func() {
			mockPM.createFunc = func(_ context.Context, _ placement.CreateResourceRequest, _ string) (*placement.Resource, error) {
				return nil, &placement.PlacementError{StatusCode: 406, Body: "policy rejected"}
			}

			instanceID := "pm-policy-fail"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Policy Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrPlacementManagerPolicyRejected)).To(BeTrue())
			Expect(result).To(BeNil())

			// Verify DB record was cleaned up (rollback)
			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})

		It("should return ErrPlacementManagerProviderError when PM create returns 422", func() {
			mockPM.createFunc = func(_ context.Context, _ placement.CreateResourceRequest, _ string) (*placement.Resource, error) {
				return nil, &placement.PlacementError{StatusCode: 422, Body: "provider error"}
			}

			instanceID := "pm-provider-fail"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Provider Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrPlacementManagerProviderError)).To(BeTrue())
			Expect(result).To(BeNil())

			// Verify DB record was cleaned up (rollback)
			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})

		It("should return ErrPlacementManagerPolicyDependency when PM create returns 424", func() {
			mockPM.createFunc = func(_ context.Context, _ placement.CreateResourceRequest, _ string) (*placement.Resource, error) {
				return nil, &placement.PlacementError{StatusCode: 424, Body: "policy dependency"}
			}

			instanceID := "pm-dependency-fail"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Dependency Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrPlacementManagerPolicyDependency)).To(BeTrue())
			Expect(result).To(BeNil())

			// Verify DB record was cleaned up (rollback)
			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})
	})

	Describe("Rehydrate with PM", func() {
		It("should update DB first then call PM rehydrate with old and new resource IDs", func() {
			var capturedOldResourceID string
			var capturedNewResourceID string
			mockPM.rehydrateFunc = func(_ context.Context, resourceID string, newResourceID string) (*placement.Resource, error) {
				capturedOldResourceID = resourceID
				capturedNewResourceID = newResourceID
				return &placement.Resource{ID: newResourceID}, nil
			}

			instanceID := "rehydrate-instance"
			created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "Rehydrate Test",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			oldResourceID := *created.ResourceId

			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// PM was called with the old resource ID
			Expect(capturedOldResourceID).To(Equal(oldResourceID))
			// New resource ID is a UUID, different from old
			Expect(capturedNewResourceID).ToNot(Equal(oldResourceID))
			Expect(capturedNewResourceID).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
			// Result has the new resource ID
			Expect(*result.ResourceId).To(Equal(capturedNewResourceID))
			// Instance ID unchanged
			Expect(*result.Uid).To(Equal(instanceID))

			Expect(mockPM.rehydrateCalls).To(Equal(1))

			// Verify persisted
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.ResourceId).To(Equal(capturedNewResourceID))
		})

		It("should return ErrCatalogItemInstanceNotFound for non-existent instance", func() {
			result, err := svc.CatalogItemInstance().Rehydrate(ctx, "nonexistent")
			Expect(err).To(Equal(service.ErrCatalogItemInstanceNotFound))
			Expect(result).To(BeNil())
			Expect(mockPM.rehydrateCalls).To(Equal(0))
		})

		It("should rollback resource_id and not call PM when a second rehydrate races", func() {
			instanceID := "rehydrate-conflict"
			created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "Conflict Test",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			// First rehydrate succeeds
			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			newResourceID := *result.ResourceId
			Expect(newResourceID).ToNot(Equal(*created.ResourceId))

			// Simulate a concurrent caller that read the old resource_id before
			// the first rehydrate committed — manually revert DB to old resource_id
			// to set up the CAS conflict scenario
			directStore := str.CatalogItemInstance()
			_, err = directStore.UpdateResourceID(ctx, instanceID, newResourceID, *created.ResourceId)
			Expect(err).ToNot(HaveOccurred())

			// Now rehydrate again — this should succeed since resource_id matches
			mockPM.rehydrateCalls = 0
			result2, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(mockPM.rehydrateCalls).To(Equal(1))
			Expect(*result2.ResourceId).ToNot(Equal(*created.ResourceId))
		})

		It("should rollback resource_id when PM rehydrate fails", func() {
			instanceID := "rehydrate-pm-fail"
			created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Rehydrate Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			oldResourceID := *created.ResourceId

			mockPM.rehydrateFunc = func(_ context.Context, _ string, _ string) (*placement.Resource, error) {
				return nil, errors.New("PM rehydrate unavailable")
			}

			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("placement manager rehydrate resource failed"))
			Expect(result).To(BeNil())

			// Verify resource_id rolled back to original
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.ResourceId).To(Equal(oldResourceID))
		})

		It("should return ErrPlacementManagerPolicyRejected when PM rehydrate returns 406", func() {
			instanceID := "rehydrate-policy-fail"
			created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Rehydrate Policy Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			oldResourceID := *created.ResourceId

			mockPM.rehydrateFunc = func(_ context.Context, _ string, _ string) (*placement.Resource, error) {
				return nil, &placement.PlacementError{StatusCode: 406, Body: "policy rejected"}
			}

			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrPlacementManagerPolicyRejected)).To(BeTrue())
			Expect(result).To(BeNil())

			// Verify resource_id rolled back
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.ResourceId).To(Equal(oldResourceID))
		})

		It("should return ErrPlacementManagerProviderError when PM rehydrate returns 422", func() {
			instanceID := "rehydrate-provider-fail"
			created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Rehydrate Provider Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			oldResourceID := *created.ResourceId

			mockPM.rehydrateFunc = func(_ context.Context, _ string, _ string) (*placement.Resource, error) {
				return nil, &placement.PlacementError{StatusCode: 422, Body: "provider error"}
			}

			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrPlacementManagerProviderError)).To(BeTrue())
			Expect(result).To(BeNil())

			// Verify resource_id rolled back
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.ResourceId).To(Equal(oldResourceID))
		})

		It("should return ErrPlacementManagerPolicyDependency when PM rehydrate returns 424", func() {
			instanceID := "rehydrate-dependency-fail"
			created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Rehydrate Dependency Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			oldResourceID := *created.ResourceId

			mockPM.rehydrateFunc = func(_ context.Context, _ string, _ string) (*placement.Resource, error) {
				return nil, &placement.PlacementError{StatusCode: 424, Body: "policy dependency"}
			}

			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrPlacementManagerPolicyDependency)).To(BeTrue())
			Expect(result).To(BeNil())

			// Verify resource_id rolled back
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.ResourceId).To(Equal(oldResourceID))
		})
	})

	Describe("Delete with PM", func() {
		It("should delete PM resource using stored resource ID then local record", func() {
			var createdResourceID string
			var deletedResourceID string
			mockPM.createFunc = func(_ context.Context, _ placement.CreateResourceRequest, id string) (*placement.Resource, error) {
				createdResourceID = id
				return &placement.Resource{ID: id}, nil
			}
			mockPM.deleteFunc = func(_ context.Context, resourceID string) error {
				deletedResourceID = resourceID
				return nil
			}

			instanceID := "delete-pm-instance"
			_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "To Delete",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			err = svc.CatalogItemInstance().Delete(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			// Delete should use the stored resource ID, not the instance ID
			Expect(deletedResourceID).ToNot(Equal(instanceID))
			Expect(deletedResourceID).To(Equal(createdResourceID))

			// Verify local record deleted
			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})

		It("should not delete local record when PM delete fails", func() {
			mockPM.createFunc = func(_ context.Context, _ placement.CreateResourceRequest, _ string) (*placement.Resource, error) {
				return &placement.Resource{ID: "pm-fail-delete"}, nil
			}

			instanceID := "pm-delete-fail"
			_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "PM Delete Fail",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			// Make PM delete fail
			mockPM.deleteFunc = func(_ context.Context, _ string) error {
				return errors.New("PM delete unavailable")
			}

			err = svc.CatalogItemInstance().Delete(ctx, instanceID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("placement manager delete resource failed"))

			// Verify local record still exists (allows retry)
			result, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})
})
