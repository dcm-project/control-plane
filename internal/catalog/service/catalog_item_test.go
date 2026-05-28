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
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

func ensureServiceType(ctx context.Context, str store.Store, id, serviceType string) {
	st := model.ServiceType{
		ID:          id,
		ApiVersion:  "v1alpha1",
		ServiceType: serviceType,
		Spec:        map[string]any{"x": 1},
		Path:        fmt.Sprintf("service-types/%s", id),
	}
	_, err := str.ServiceType().Create(ctx, st)
	if err != nil {
		// May already exist (duplicate id or service_type)
		return
	}
}

var _ = Describe("CatalogItem Service", func() {
	var (
		ctx                  context.Context
		db                   *gorm.DB
		str                  store.Store
		svc                  service.Service
		serviceTypeVM        = "vm"
		serviceTypeContainer = "container"
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
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{})
		Expect(err).ToNot(HaveOccurred())
		str = store.NewStore(db, slog.Default())
		svc, err = service.NewService(str, &mockPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
		// Ensure service types exist for catalog item FK
		ensureServiceType(ctx, str, "vm-st", "vm")
		ensureServiceType(ctx, str, "container-st", "container")
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	Describe("Create", func() {
		Context("with valid user-provided DNS-1123 ID", func() {
			It("should create a catalog item with the provided ID", func() {
				userID := "my-catalog-item"
				displayName := "Test Catalog Item"
				req := &service.CreateCatalogItemRequest{
					ID:          &userID,
					ApiVersion:  "v1alpha1",
					DisplayName: displayName,
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "spec.vcpu.count", Default: 2},
						},
					},
				}

				result, err := svc.CatalogItem().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Uid).To(Equal(userID))
				Expect(*result.DisplayName).To(Equal(displayName))
				Expect(*result.Spec.ServiceType).To(Equal(serviceTypeVM))
				Expect(*result.Spec.Fields).To(HaveLen(1))
			})
		})

		Context("without ID (auto-generate UUID)", func() {
			It("should auto-generate a UUID for the catalog item", func() {
				req := &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Auto ID Item",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeContainer,
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "spec.image", Default: "nginx"},
						},
					},
				}

				result, err := svc.CatalogItem().Create(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Uid).ToNot(BeNil())
				Expect(*result.Uid).To(MatchRegexp(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`))
			})
		})

		Context("when store returns duplicate ID error", func() {
			It("should return ErrCatalogItemIDTaken", func() {
				id := "taken-id"
				req1 := &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "First",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "spec.vcpu", Default: 2},
						},
					},
				}
				_, err := svc.CatalogItem().Create(ctx, req1)
				Expect(err).ToNot(HaveOccurred())

				req2 := &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Second",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeContainer,
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "spec.image", Default: "nginx"},
						},
					},
				}
				result, err := svc.CatalogItem().Create(ctx, req2)
				Expect(err).To(Equal(service.ErrCatalogItemIDTaken))
				Expect(result).To(BeNil())
			})
		})

		Context("when store returns service type not found error", func() {
			It("should return ErrServiceTypeNotFound", func() {
				serviceTypeNonexistent := "nonexistent"
				req := &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Nonexistent Service Type",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeNonexistent,
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "spec.vcpu", Default: 2},
						},
					},
				}
				result, err := svc.CatalogItem().Create(ctx, req)
				Expect(err).To(Equal(service.ErrServiceTypeNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("List", func() {
		Context("without filters", func() {
			It("should return all catalog items", func() {
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Item 1",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Item 2",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeContainer,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.image", Default: "nginx"}},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(2))
			})
		})

		Context("with service_type filter", func() {
			It("should filter by service_type", func() {
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "VM Item",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				_, err = svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Container Item",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeContainer,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.image", Default: "nginx"}},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				svcType := "vm"
				result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{ServiceType: &svcType})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(1))
				Expect(*result.CatalogItems[0].Spec.ServiceType).To(Equal(serviceTypeVM))
			})
		})

		Context("with pagination options", func() {
			It("should pass pagination parameters and return next page token when more results exist", func() {
				for i := range 6 {
					_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
						ApiVersion:  "v1alpha1",
						DisplayName: fmt.Sprintf("Item %d", i),
						Spec: v1alpha1.CatalogItemSpec{
							ServiceType: &serviceTypeVM,
							Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
						},
					})
					Expect(err).ToNot(HaveOccurred())
				}

				maxPageSize := int32(2)
				result, err := svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
					MaxPageSize: &maxPageSize,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(2))
				Expect(result.NextPageToken).ToNot(BeNil())

				maxPageSize = int32(3)
				result, err = svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
					MaxPageSize: &maxPageSize,
					PageToken:   result.NextPageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(3))
				Expect(result.NextPageToken).ToNot(BeNil())

				maxPageSize = int32(4)
				result, err = svc.CatalogItem().List(ctx, service.CatalogItemListOptions{
					MaxPageSize: &maxPageSize,
					PageToken:   result.NextPageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.CatalogItems).To(HaveLen(1))
				Expect(result.NextPageToken).To(BeNil())
			})
		})
	})

	Describe("Get", func() {
		Context("with valid ID", func() {
			It("should return the catalog item", func() {
				created, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ApiVersion:  "v1alpha1",
					DisplayName: "Test Item",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(created.Uid).ToNot(BeNil())

				result, err := svc.CatalogItem().Get(ctx, *created.Uid)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Uid).To(Equal(*created.Uid))
				Expect(*result.DisplayName).To(Equal("Test Item"))
			})
		})

		Context("with non-existent ID", func() {
			It("should return ErrCatalogItemNotFound", func() {
				result, err := svc.CatalogItem().Get(ctx, "nonexistent")
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Update", func() {
		Context("updating display_name only", func() {
			It("should update the display_name", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Old Name",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				newDisplayName := "Updated Name"
				req := &service.UpdateCatalogItemRequest{
					DisplayName: &newDisplayName,
				}

				result, err := svc.CatalogItem().Update(ctx, "item1", req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.DisplayName).To(Equal(newDisplayName))
			})
		})

		Context("updating spec.fields only", func() {
			It("should update the spec fields", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Name",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				newSpec := &v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeVM,
					Fields: &[]v1alpha1.FieldConfiguration{
						{Path: "spec.vcpu", Default: 4},
						{Path: "spec.memory", Default: "8GB"},
					},
				}
				req := &service.UpdateCatalogItemRequest{
					Spec: newSpec,
				}

				result, err := svc.CatalogItem().Update(ctx, "item1", req)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result.Spec.Fields).To(HaveLen(2))
			})
		})

		Context("attempting to update spec.service_type (immutable)", func() {
			It("should return ErrImmutableFieldUpdate", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Name",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				newSpec := &v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeContainer,
					Fields: &[]v1alpha1.FieldConfiguration{
						{Path: "spec.image", Default: "nginx"},
					},
				}
				req := &service.UpdateCatalogItemRequest{
					Spec: newSpec,
				}

				result, err := svc.CatalogItem().Update(ctx, "item1", req)
				Expect(err).To(Equal(service.ErrImmutableFieldUpdate))
				Expect(result).To(BeNil())
			})
		})

		Context("with non-existent item", func() {
			It("should return ErrCatalogItemNotFound", func() {
				newName := "New Name"
				req := &service.UpdateCatalogItemRequest{
					DisplayName: &newName,
				}

				result, err := svc.CatalogItem().Update(ctx, "nonexistent", req)
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Create with cyclic depends_on", func() {
		It("should reject fields with cyclic depends_on references", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Cyclic DependsOn",
				Spec: v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeVM,
					Fields: &[]v1alpha1.FieldConfiguration{
						{
							Path:     "spec.vcpu.count",
							Default:  float64(2),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.memory.size_gb",
								AllowedValues: map[string][]any{
									"4": {float64(2), float64(4)},
								},
							},
						},
						{
							Path:     "spec.memory.size_gb",
							Default:  float64(4),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.vcpu.count",
								AllowedValues: map[string][]any{
									"2": {float64(4), float64(8)},
								},
							},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})

		It("should reject a three-field cycle (A -> B -> C -> A)", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Three-Field Cycle",
				Spec: v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeVM,
					Fields: &[]v1alpha1.FieldConfiguration{
						{
							Path:     "spec.vcpu.count",
							Default:  float64(2),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.memory.size_gb",
								AllowedValues: map[string][]any{
									"4": {float64(2), float64(4)},
								},
							},
						},
						{
							Path:     "spec.memory.size_gb",
							Default:  float64(4),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.disk.size_gb",
								AllowedValues: map[string][]any{
									"100": {float64(4), float64(8)},
								},
							},
						},
						{
							Path:     "spec.disk.size_gb",
							Default:  float64(100),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.vcpu.count",
								AllowedValues: map[string][]any{
									"2": {float64(100), float64(200)},
								},
							},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})

		It("should accept fields without cyclic depends_on references", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Valid DependsOn",
				Spec: v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeVM,
					Fields: &[]v1alpha1.FieldConfiguration{
						{
							Path:     "spec.vcpu.count",
							Default:  float64(2),
							Editable: &editable,
						},
						{
							Path:     "spec.memory.size_gb",
							Default:  float64(4),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.vcpu.count",
								AllowedValues: map[string][]any{
									"2": {float64(4), float64(8)},
									"4": {float64(8), float64(16)},
								},
							},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})

	Describe("Create with invalid depends_on path", func() {
		It("should reject when depends_on path does not reference an existing field", func() {
			editable := true
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Invalid DependsOn Path",
				Spec: v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeVM,
					Fields: &[]v1alpha1.FieldConfiguration{
						{
							Path:     "spec.memory.size_gb",
							Default:  float64(4),
							Editable: &editable,
							DependsOn: &v1alpha1.FieldConfigurationDependsOn{
								Path: "spec.region",
								AllowedValues: map[string][]any{
									"us-central1": {float64(4), float64(8)},
								},
							},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrDependsOnPathNotFound)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("spec.region"))
			Expect(err.Error()).To(ContainSubstring("not found"))
			Expect(result).To(BeNil())
		})
	})

	Describe("Update with cyclic depends_on", func() {
		It("should reject update that introduces cyclic depends_on", func() {
			id := "item-cycle-update"
			editable := true
			_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
				ID:          &id,
				ApiVersion:  "v1alpha1",
				DisplayName: "No Cycle",
				Spec: v1alpha1.CatalogItemSpec{
					ServiceType: &serviceTypeVM,
					Fields: &[]v1alpha1.FieldConfiguration{
						{Path: "spec.vcpu.count", Default: float64(2), Editable: &editable},
						{Path: "spec.memory.size_gb", Default: float64(4), Editable: &editable},
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())

			updateSpec := &v1alpha1.CatalogItemSpec{
				ServiceType: &serviceTypeVM,
				Fields: &[]v1alpha1.FieldConfiguration{
					{
						Path:     "spec.vcpu.count",
						Default:  float64(2),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.memory.size_gb",
							AllowedValues: map[string][]any{
								"4": {float64(2), float64(4)},
							},
						},
					},
					{
						Path:     "spec.memory.size_gb",
						Default:  float64(4),
						Editable: &editable,
						DependsOn: &v1alpha1.FieldConfigurationDependsOn{
							Path: "spec.vcpu.count",
							AllowedValues: map[string][]any{
								"2": {float64(4), float64(8)},
							},
						},
					},
				},
			}

			result, err := svc.CatalogItem().Update(ctx, id, &service.UpdateCatalogItemRequest{
				Spec: updateSpec,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cycle"))
			Expect(result).To(BeNil())
		})
	})

	Describe("Delete", func() {
		Context("with existing item", func() {
			It("should delete the catalog item", func() {
				id := "item1"
				_, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
					ID:          &id,
					ApiVersion:  "v1alpha1",
					DisplayName: "To Delete",
					Spec: v1alpha1.CatalogItemSpec{
						ServiceType: &serviceTypeVM,
						Fields:      &[]v1alpha1.FieldConfiguration{{Path: "spec.vcpu", Default: 2}},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				err = svc.CatalogItem().Delete(ctx, "item1")
				Expect(err).ToNot(HaveOccurred())

				_, err = svc.CatalogItem().Get(ctx, "item1")
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
			})
		})

		Context("with non-existent item", func() {
			It("should return ErrCatalogItemNotFound", func() {
				err := svc.CatalogItem().Delete(ctx, "nonexistent")
				Expect(err).To(Equal(service.ErrCatalogItemNotFound))
			})
		})
	})
})
