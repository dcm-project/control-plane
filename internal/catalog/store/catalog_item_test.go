package store_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

var _ = Describe("CatalogItem Store", func() {
	var (
		db                    *gorm.DB
		catalogItemStore      store.CatalogItemStore
		serviceTypeStore      store.ServiceTypeStore
		createTestServiceType func(id, serviceType string)
	)

	BeforeEach(func() {
		// Create in-memory SQLite database
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())

		// Enable foreign key constraints in SQLite
		err = db.Exec("PRAGMA foreign_keys = ON").Error
		Expect(err).ToNot(HaveOccurred())

		// Auto-migrate parent models first to create foreign key constraints
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{})
		Expect(err).ToNot(HaveOccurred())

		catalogItemStore = store.NewCatalogItemStore(db, slog.Default())
		serviceTypeStore = store.NewServiceTypeStore(db, slog.Default())

		// Helper function to create prerequisite ServiceTypes
		createTestServiceType = func(id, serviceType string) {
			st := model.ServiceType{
				ID:          id,
				ApiVersion:  "v1alpha1",
				ServiceType: serviceType,
				Spec:        map[string]any{},
				Path:        fmt.Sprintf("service-types/%s", id),
			}
			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		sqlDB, err := db.DB()
		Expect(err).ToNot(HaveOccurred())
		_ = sqlDB.Close()
	})

	Describe("Create", func() {
		It("should create a new catalog item successfully", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st", "vm")

			ci := &model.CatalogItem{
				ID:          "small-vm",
				ApiVersion:  "v1alpha1",
				DisplayName: "Small VM",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields: []model.FieldConfiguration{
						{
							Path:     "spec.vcpu.count",
							Editable: false,
							Default:  2,
						},
					},
				},
				Path: "catalog-items/small-vm",
			}

			_, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was created
			retrieved, err := catalogItemStore.Get(context.Background(), "small-vm")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal("small-vm"))
			Expect(retrieved.DisplayName).To(Equal("Small VM"))
			Expect(retrieved.Spec.ServiceType).To(Equal("vm"))
			Expect(retrieved.SpecServiceType).To(Equal("vm"))
		})

		It("should return error when creating duplicate ID", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st", "vm")

			ci := &model.CatalogItem{
				ID:          "duplicate-ci",
				ApiVersion:  "v1alpha1",
				DisplayName: "Original",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/duplicate-ci",
			}

			_, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())

			// Try to create again with same ID
			ci2 := model.CatalogItem{
				ID:          "duplicate-ci",
				ApiVersion:  "v1alpha1",
				DisplayName: "Duplicate",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/duplicate-ci",
			}

			_, err = catalogItemStore.Create(context.Background(), ci2)
			Expect(err).To(Equal(store.ErrCatalogItemIDTaken))
		})

		It("should return error when creating with non-existent service type", func() {
			ci := &model.CatalogItem{
				ID:          "invalid-st-ci",
				ApiVersion:  "v1alpha1",
				DisplayName: "Invalid Service Type",
				Spec: model.CatalogItemSpec{
					ServiceType: "non-existent-service-type",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/invalid-st-ci",
			}

			_, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).To(Equal(store.ErrServiceTypeNotFound))
		})

		It("should create catalog item with valid service type", func() {
			// Create prerequisite service type
			createTestServiceType("valid-st", "valid-service")

			ci := &model.CatalogItem{
				ID:          "valid-ci",
				ApiVersion:  "v1alpha1",
				DisplayName: "Valid Catalog Item",
				Spec: model.CatalogItemSpec{
					ServiceType: "valid-service",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/valid-ci",
			}

			_, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Get", func() {
		It("should retrieve an existing catalog item", func() {
			// Create prerequisite service type
			createTestServiceType("db-st", "database")

			ci := &model.CatalogItem{
				ID:          "get-test-ci",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Item",
				Spec: model.CatalogItemSpec{
					ServiceType: "database",
					Fields: []model.FieldConfiguration{
						{Path: "spec.engine", Default: "postgres"},
					},
				},
				Path: "catalog-items/get-test-ci",
			}

			_, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())

			retrieved, err := catalogItemStore.Get(context.Background(), "get-test-ci")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal("get-test-ci"))
			Expect(retrieved.Spec.ServiceType).To(Equal("database"))
		})

		It("should return error for non-existent catalog item", func() {
			_, err := catalogItemStore.Get(context.Background(), "non-existent")
			Expect(err).To(Equal(store.ErrCatalogItemNotFound))
		})
	})

	Describe("Update", func() {
		It("should update mutable fields successfully", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st-update", "vm")

			ci := &model.CatalogItem{
				ID:          "update-test",
				ApiVersion:  "v1alpha1",
				DisplayName: "Original Name",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields: []model.FieldConfiguration{
						{Path: "spec.vcpu.count", Default: 2},
					},
				},
				Path: "catalog-items/update-test",
			}

			created, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())
			ci = created

			// Update mutable fields
			ci.DisplayName = "Updated Name"
			ci.Spec.Fields = append(ci.Spec.Fields, model.FieldConfiguration{
				Path:    "spec.memory.size_gb",
				Default: 8,
			})

			err = catalogItemStore.Update(context.Background(), ci)
			Expect(err).ToNot(HaveOccurred())

			// Verify update
			retrieved, err := catalogItemStore.Get(context.Background(), "update-test")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.DisplayName).To(Equal("Updated Name"))
			Expect(retrieved.Spec.Fields).To(HaveLen(2))
		})

		It("should not update immutable fields", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st-immutable", "vm")

			originalPath := "catalog-items/immutable-update-test"
			originalApiVersion := "v1alpha1"
			ci := &model.CatalogItem{
				ID:          "immutable-update-test",
				ApiVersion:  originalApiVersion,
				DisplayName: "Original Name",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
				Path: originalPath,
			}

			created, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())
			ci = created

			// Attempt to update immutable fields (and one mutable field to show update runs)
			ci.Path = "catalog-items/tampered-path"
			ci.ApiVersion = "v1beta1"
			ci.DisplayName = "Updated Name"

			err = catalogItemStore.Update(context.Background(), ci)
			Expect(err).ToNot(HaveOccurred())

			// Verify immutable fields are unchanged
			retrieved, err := catalogItemStore.Get(context.Background(), "immutable-update-test")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.Path).To(Equal(originalPath))
			Expect(retrieved.ApiVersion).To(Equal(originalApiVersion))
			Expect(retrieved.DisplayName).To(Equal("Updated Name"))
		})

		It("should return error when updating non-existent catalog item", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st-nonexist", "vm")

			ci := &model.CatalogItem{
				ID:          "non-existent",
				DisplayName: "Updated",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
			}

			err := catalogItemStore.Update(context.Background(), ci)
			Expect(err).To(Equal(store.ErrCatalogItemNotFound))
		})

		It("should return error when updating with non-existent service type", func() {
			// Create prerequisite service types
			createTestServiceType("vm-st-orig", "vm")

			ci := &model.CatalogItem{
				ID:          "update-invalid-st",
				ApiVersion:  "v1alpha1",
				DisplayName: "Original",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/update-invalid-st",
			}

			created, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())
			ci = created

			// Try to update with non-existent service type
			ci.Spec.ServiceType = "non-existent-service-type"
			err = catalogItemStore.Update(context.Background(), ci)
			Expect(err).To(Equal(store.ErrServiceTypeNotFound))
		})
	})

	Describe("Delete", func() {
		It("should delete an existing catalog item", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st-del", "vm")

			ci := &model.CatalogItem{
				ID:          "delete-test",
				ApiVersion:  "v1alpha1",
				DisplayName: "To Delete",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/delete-test",
			}

			_, err := catalogItemStore.Create(context.Background(), *ci)
			Expect(err).ToNot(HaveOccurred())

			err = catalogItemStore.Delete(context.Background(), "delete-test")
			Expect(err).ToNot(HaveOccurred())

			// Verify deletion
			_, err = catalogItemStore.Get(context.Background(), "delete-test")
			Expect(err).To(Equal(store.ErrCatalogItemNotFound))
		})

		It("should return error when deleting non-existent catalog item", func() {
			err := catalogItemStore.Delete(context.Background(), "non-existent")
			Expect(err).To(Equal(store.ErrCatalogItemNotFound))
		})

		// Note: Test for deleting with existing instances is in integration_test.go
		// because it requires creating CatalogItemInstance records
	})

	Describe("List", func() {
		It("should return empty list when no catalog items exist", func() {
			result, err := catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{PageSize: 100})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(BeEmpty())
			Expect(result.NextPageToken).To(BeNil())
		})

		It("should list all catalog items", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st-list", "vm")

			// Create multiple catalog items
			for i := 1; i <= 3; i++ {
				ci := model.CatalogItem{
					ID:          fmt.Sprintf("ci-%d", i),
					ApiVersion:  "v1alpha1",
					DisplayName: fmt.Sprintf("Item %d", i),
					Spec: model.CatalogItemSpec{
						ServiceType: "vm",
						Fields:      []model.FieldConfiguration{},
					},
					Path: fmt.Sprintf("catalog-items/ci-%d", i),
				}
				time.Sleep(time.Millisecond)
				_, err := catalogItemStore.Create(context.Background(), ci)
				Expect(err).ToNot(HaveOccurred())
			}

			result, err := catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{PageSize: 100})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(HaveLen(3))
			Expect(result.NextPageToken).To(BeNil())
		})

		It("should filter by service type", func() {
			// Create prerequisite service types
			createTestServiceType("vm-st-filter", "vm")
			createTestServiceType("db-st-filter", "database")

			// Create catalog items with different service types
			ci1 := model.CatalogItem{
				ID:          "vm-item",
				ApiVersion:  "v1alpha1",
				DisplayName: "VM Item",
				Spec: model.CatalogItemSpec{
					ServiceType: "vm",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/vm-item",
			}
			_, err := catalogItemStore.Create(context.Background(), ci1)
			Expect(err).ToNot(HaveOccurred())

			ci2 := model.CatalogItem{
				ID:          "db-item",
				ApiVersion:  "v1alpha1",
				DisplayName: "DB Item",
				Spec: model.CatalogItemSpec{
					ServiceType: "database",
					Fields:      []model.FieldConfiguration{},
				},
				Path: "catalog-items/db-item",
			}
			_, err = catalogItemStore.Create(context.Background(), ci2)
			Expect(err).ToNot(HaveOccurred())

			// Filter for vm service type
			serviceTypeVM := "vm"
			result, err := catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{PageSize: 100, ServiceType: &serviceTypeVM})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(HaveLen(1))
			Expect(result.CatalogItems[0].Spec.ServiceType).To(Equal("vm"))

			// Filter for database service type
			serviceTypeDB := "database"
			result, err = catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{PageSize: 100, ServiceType: &serviceTypeDB})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(HaveLen(1))
			Expect(result.CatalogItems[0].Spec.ServiceType).To(Equal("database"))

			// Filter for non-existent service type
			serviceTypeNonExistent := "non-existent"
			result, err = catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{PageSize: 100, ServiceType: &serviceTypeNonExistent})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.CatalogItems).To(BeEmpty())
		})

		It("should handle pagination correctly", func() {
			// Create prerequisite service type
			createTestServiceType("vm-st-page", "vm")

			// Create 6 catalog items
			for i := 1; i <= 6; i++ {
				ci := model.CatalogItem{
					ID:          fmt.Sprintf("page-ci-%d", i),
					ApiVersion:  "v1alpha1",
					DisplayName: fmt.Sprintf("Item %d", i),
					Spec: model.CatalogItemSpec{
						ServiceType: "vm",
						Fields:      []model.FieldConfiguration{},
					},
					Path: fmt.Sprintf("catalog-items/page-ci-%d", i),
				}
				time.Sleep(time.Millisecond)
				_, err := catalogItemStore.Create(context.Background(), ci)
				Expect(err).ToNot(HaveOccurred())
			}

			var pageToken *string
			for _, pageSize := range []int{3, 2} {
				results, err := catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{
					PageSize:  pageSize,
					PageToken: pageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(results.CatalogItems).To(HaveLen(pageSize))
				Expect(results.NextPageToken).ToNot(BeNil())
				pageToken = results.NextPageToken
			}

			// Get last page (should have 1 item)
			lastPageResults, err := catalogItemStore.List(context.Background(), &store.CatalogItemListOptions{
				PageToken: pageToken,
				PageSize:  4,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(lastPageResults.CatalogItems).To(HaveLen(1))
			Expect(lastPageResults.NextPageToken).To(BeNil())
		})
	})
})
