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
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
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

func seedCatalogItemInstance(ctx context.Context, str store.Store, id string, resourceIDs []string) {
	_, err := str.CatalogItemInstance().Create(ctx, model.CatalogItemInstance{
		ID:          id,
		ApiVersion:  "v1alpha1",
		DisplayName: "Seeded instance",
		Spec: model.CatalogItemInstanceSpec{
			CatalogItemId: "small-vm",
			UserValues:    []model.UserValue{},
			ResourceIDs:   append([]string(nil), resourceIDs...),
		},
		Path:              fmt.Sprintf("catalog-item-instances/%s", id),
		SpecCatalogItemId: "small-vm",
	})
	if err != nil {
		panic(err)
	}
}

func ensureCatalogItem(ctx context.Context, str store.Store, id, serviceType string) {
	ci := model.CatalogItem{
		ID:          id,
		ApiVersion:  "v1alpha1",
		DisplayName: fmt.Sprintf("Test %s", id),
		Spec:        testutil.ModelCatalogSpec(serviceType, []model.FieldConfiguration{}),
		Path:        fmt.Sprintf("catalog-items/%s", id),
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
		Spec:        testutil.ModelCatalogSpec(serviceType, fields),
		Path:        fmt.Sprintf("catalog-items/%s", id),
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

func ensureMultiResourceCatalogItem(ctx context.Context, str store.Store, id string, resources []model.CatalogResource) {
	ci := model.CatalogItem{
		ID:          id,
		ApiVersion:  "v1alpha1",
		DisplayName: fmt.Sprintf("Test %s", id),
		Spec: model.CatalogItemSpec{
			Resources: resources,
		},
		Path: fmt.Sprintf("catalog-items/%s", id),
	}
	_, err := str.CatalogItem().Create(ctx, ci)
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
				Expect(result.Spec.ResourceIds).ToNot(BeNil())
				Expect(*result.Spec.ResourceIds).To(HaveLen(1))
				Expect((*result.Spec.ResourceIds)[0]).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
				Expect(mockPM.createCalls).To(Equal(1))
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
				Expect(result.Spec.ResourceIds).ToNot(BeNil())
				Expect(*result.Spec.ResourceIds).To(HaveLen(1))
				Expect((*result.Spec.ResourceIds)[0]).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
				Expect(mockPM.createCalls).To(Equal(1))
				Expect((*result.Spec.ResourceIds)[0]).ToNot(Equal(*result.Uid))
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
							{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
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
							{Resource: testutil.DefaultResourceName, Path: "spec.disk.size", Value: float64(100)},
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
							{Resource: testutil.DefaultResourceName, Path: "spec.memory.size_gb", Value: float64(16)},
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
							{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(32)},
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
							{Resource: testutil.DefaultResourceName, Path: "spec.vcpu.count", Value: float64(8)},
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

		Context("multi-resource catalog item", func() {
			BeforeEach(func() {
				ensureServiceTypeWithSpec(ctx, str, "db-st", "database", map[string]any{
					"engine":  "postgres",
					"version": "14",
				})
				ensureMultiResourceCatalogItem(ctx, str, "dev-app", []model.CatalogResource{
					{
						Name:        "ordersDb",
						ServiceType: "database",
						Fields: []model.FieldConfiguration{
							{Path: "engine", Default: "postgres", Editable: true},
							{Path: "version", Default: "16", Editable: true},
						},
					},
					{
						Name:              "app",
						ServiceType:       "container",
						RequiresResources: []string{"ordersDb"},
						Fields: []model.FieldConfiguration{
							{Path: "image", Default: "registry.example.com/app:1.0"},
						},
					},
				})
			})

			It("should assign resource_ids for each resolved resource and call PM once", func() {
				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Dev App Instance",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "dev-app",
						UserValues:    []v1alpha1.UserValue{},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result.Spec.CatalogItemId).To(Equal("dev-app"))
				Expect(mockPM.createCalls).To(Equal(1))
				Expect(result.Spec.ResourceIds).ToNot(BeNil())
				Expect(*result.Spec.ResourceIds).To(HaveLen(2))
				for _, rid := range *result.Spec.ResourceIds {
					Expect(rid).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
				}
			})

			It("should accept user_values with resource and path", func() {
				resource := "ordersDb"
				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Dev App Override",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "dev-app",
						UserValues: []v1alpha1.UserValue{
							{Resource: resource, Path: "version", Value: "17"},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(mockPM.createCalls).To(Equal(1))
			})

			It("should reject user_value without resource", func() {
				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Missing resource",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "dev-app",
						UserValues: []v1alpha1.UserValue{
							{Path: "version", Value: "17"},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).To(Equal(service.ErrUserValueResourceRequired))
				Expect(result).To(BeNil())
				Expect(mockPM.createCalls).To(Equal(0))
			})

			It("should reject user_value for unknown resource", func() {
				resource := "unknown"
				req := &service.CreateCatalogItemInstanceRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Bad resource",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "dev-app",
						UserValues: []v1alpha1.UserValue{
							{Resource: resource, Path: "version", Value: "17"},
						},
					},
				}

				result, err := svc.CatalogItemInstance().Create(ctx, req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("user value resource not found"))
				Expect(result).To(BeNil())
				Expect(mockPM.createCalls).To(Equal(0))
			})

			It("should delete the first placement resource", func() {
				instanceID := "multi-resource-delete"
				_, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ID:          &instanceID,
					ApiVersion:  "v1alpha1",
					DisplayName: "Multi-resource Delete",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "dev-app",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				mockPM.deleteCalls = 0

				err = svc.CatalogItemInstance().Delete(ctx, instanceID)
				Expect(err).ToNot(HaveOccurred())
				Expect(mockPM.deleteCalls).To(Equal(1))

				_, err = svc.CatalogItemInstance().Get(ctx, instanceID)
				Expect(err).To(Equal(service.ErrCatalogItemInstanceNotFound))
			})

			It("should rehydrate only the first placement resource", func() {
				instanceID := "multi-resource-rehydrate"
				created, err := svc.CatalogItemInstance().Create(ctx, &service.CreateCatalogItemInstanceRequest{
					ID:          &instanceID,
					ApiVersion:  "v1alpha1",
					DisplayName: "Multi-resource Rehydrate",
					Spec: v1alpha1.CatalogItemInstanceSpec{
						CatalogItemId: "dev-app",
						UserValues:    []v1alpha1.UserValue{},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(created.Spec.ResourceIds).ToNot(BeNil())
				Expect(*created.Spec.ResourceIds).To(HaveLen(2))
				originalResourceIDs := append([]string(nil), *created.Spec.ResourceIds...)

				result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Spec.ResourceIds).ToNot(BeNil())
				Expect(*result.Spec.ResourceIds).To(HaveLen(2))
				Expect((*result.Spec.ResourceIds)[0]).NotTo(Equal(originalResourceIDs[0]))
				Expect((*result.Spec.ResourceIds)[1]).To(Equal(originalResourceIDs[1]))
				Expect(mockPM.rehydrateCalls).To(Equal(1))
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
				Expect(result.Spec.ResourceIds).ToNot(BeNil())
				Expect(*result.Spec.ResourceIds).To(Equal(*created.Spec.ResourceIds))
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
		It("should persist instance and call PM for each resolved resource", func() {
			instanceID := "graph-pending-instance"
			req := &service.CreateCatalogItemInstanceRequest{
				ID:          &instanceID,
				ApiVersion:  "v1alpha1",
				DisplayName: "Graph Pending",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []v1alpha1.UserValue{},
				},
			}

			result, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(mockPM.createCalls).To(Equal(1))
			Expect(result.Spec.ResourceIds).ToNot(BeNil())
			Expect(*result.Spec.ResourceIds).To(HaveLen(1))
			Expect((*result.Spec.ResourceIds)[0]).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))

			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.Spec.ResourceIds).To(Equal(*result.Spec.ResourceIds))
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
			oldResourceID := "resource-before-rehydrate"
			seedCatalogItemInstance(ctx, str, instanceID, []string{oldResourceID})

			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// PM was called with the old resource ID
			Expect(capturedOldResourceID).To(Equal(oldResourceID))
			// New resource ID is a UUID, different from old
			Expect(capturedNewResourceID).ToNot(Equal(oldResourceID))
			Expect(capturedNewResourceID).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
			// Result has the new resource ID
			Expect(*result.Spec.ResourceIds).To(Equal([]string{capturedNewResourceID}))

			Expect(mockPM.rehydrateCalls).To(Equal(1))

			// Verify persisted
			got, err := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(*got.Spec.ResourceIds).To(Equal(*result.Spec.ResourceIds))
		})

		It("should return ErrCatalogItemInstanceNotFound for non-existent instance", func() {
			result, err := svc.CatalogItemInstance().Rehydrate(ctx, "nonexistent")
			Expect(err).To(Equal(service.ErrCatalogItemInstanceNotFound))
			Expect(result).To(BeNil())
			Expect(mockPM.rehydrateCalls).To(Equal(0))
		})

		It("should rollback resource_id and not call PM when a second rehydrate races", func() {
			instanceID := "rehydrate-conflict"
			oldResourceID := "resource-conflict-old"
			seedCatalogItemInstance(ctx, str, instanceID, []string{oldResourceID})

			// First rehydrate succeeds
			result, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			newResourceIDs := *result.Spec.ResourceIds
			Expect(newResourceIDs[0]).ToNot(Equal(oldResourceID))

			// Simulate a concurrent caller that read the old resource_id before
			// the first rehydrate committed — manually revert DB to old resource_id
			// to set up the CAS conflict scenario
			directStore := str.CatalogItemInstance()
			_, err = directStore.UpdateResourceID(ctx, instanceID, newResourceIDs[0], oldResourceID)
			Expect(err).ToNot(HaveOccurred())

			// Now rehydrate again — this should succeed since resource_id matches
			mockPM.rehydrateCalls = 0
			result2, err := svc.CatalogItemInstance().Rehydrate(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(mockPM.rehydrateCalls).To(Equal(1))
			Expect(*result2.Spec.ResourceIds).ToNot(Equal([]string{oldResourceID}))
		})

		It("should rollback resource_id when PM rehydrate fails", func() {
			instanceID := "rehydrate-pm-fail"
			oldResourceID := "resource-rehydrate-pm-fail"
			seedCatalogItemInstance(ctx, str, instanceID, []string{oldResourceID})

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
			Expect(*got.Spec.ResourceIds).To(Equal([]string{oldResourceID}))
		})

		It("should return ErrPlacementManagerPolicyRejected when PM rehydrate returns 406", func() {
			instanceID := "rehydrate-policy-fail"
			oldResourceID := "resource-rehydrate-policy-fail"
			seedCatalogItemInstance(ctx, str, instanceID, []string{oldResourceID})

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
			Expect(*got.Spec.ResourceIds).To(Equal([]string{oldResourceID}))
		})

		It("should return ErrPlacementManagerProviderError when PM rehydrate returns 422", func() {
			instanceID := "rehydrate-provider-fail"
			oldResourceID := "resource-rehydrate-provider-fail"
			seedCatalogItemInstance(ctx, str, instanceID, []string{oldResourceID})

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
			Expect(*got.Spec.ResourceIds).To(Equal([]string{oldResourceID}))
		})

		It("should return ErrPlacementManagerPolicyDependency when PM rehydrate returns 424", func() {
			instanceID := "rehydrate-dependency-fail"
			oldResourceID := "resource-rehydrate-dependency-fail"
			seedCatalogItemInstance(ctx, str, instanceID, []string{oldResourceID})

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
			Expect(*got.Spec.ResourceIds).To(Equal([]string{oldResourceID}))
		})
	})

	Describe("Delete with PM", func() {
		It("should delete PM resource using stored resource ID then local record", func() {
			var deletedResourceID string
			storedResourceID := "resource-delete-pm"
			mockPM.deleteFunc = func(_ context.Context, resourceID string) error {
				deletedResourceID = resourceID
				return nil
			}

			instanceID := "delete-pm-instance"
			seedCatalogItemInstance(ctx, str, instanceID, []string{storedResourceID})

			err := svc.CatalogItemInstance().Delete(ctx, instanceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(deletedResourceID).To(Equal(storedResourceID))

			// Verify local record deleted
			_, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).To(Equal(service.ErrCatalogItemInstanceNotFound))
		})

		It("should not delete local record when PM delete fails", func() {
			instanceID := "pm-delete-fail"
			seedCatalogItemInstance(ctx, str, instanceID, []string{"resource-pm-delete-fail"})

			// Make PM delete fail
			mockPM.deleteFunc = func(_ context.Context, _ string) error {
				return errors.New("PM delete unavailable")
			}

			err := svc.CatalogItemInstance().Delete(ctx, instanceID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("placement manager delete resource failed"))

			// Verify local record still exists (allows retry)
			result, getErr := svc.CatalogItemInstance().Get(ctx, instanceID)
			Expect(getErr).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})
})
