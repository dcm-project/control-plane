package store_test

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
	"github.com/dcm-project/control-plane/internal/catalog/testutil"
)

var _ = Describe("Foreign Key Constraint Integration Tests", func() {
	var (
		db                       *gorm.DB
		serviceTypeStore         store.ServiceTypeStore
		catalogItemStore         store.CatalogItemStore
		catalogItemInstanceStore store.CatalogItemInstanceStore
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

		// Auto-migrate all models to create foreign key constraints
		err = db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})
		Expect(err).ToNot(HaveOccurred())

		serviceTypeStore = store.NewServiceTypeStore(db, slog.Default())
		catalogItemStore = store.NewCatalogItemStore(db, slog.Default())
		catalogItemInstanceStore = store.NewCatalogItemInstanceStore(db, slog.Default())
	})

	AfterEach(func() {
		sqlDB, err := db.DB()
		Expect(err).ToNot(HaveOccurred())
		_ = sqlDB.Close()
	})

	Describe("Full Hierarchy Creation", func() {
		It("should create full hierarchy successfully", func() {
			ctx := context.Background()

			// Create ServiceType
			st := model.ServiceType{
				ID:          "vm-service",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/vm-service",
			}
			_, err := serviceTypeStore.Create(ctx, st)
			Expect(err).ToNot(HaveOccurred())

			// Create CatalogItem
			ci := model.CatalogItem{
				ID:          "small-vm",
				ApiVersion:  "v1alpha1",
				DisplayName: "Small VM",
				Spec:        testutil.ModelCatalogSpec("vm", []model.FieldConfiguration{}),
				Path:        "catalog-items/small-vm",
			}
			_, err = catalogItemStore.Create(ctx, ci)
			Expect(err).ToNot(HaveOccurred())

			// Create CatalogItemInstance
			cii := model.CatalogItemInstance{
				ID:          "my-vm",
				ApiVersion:  "v1alpha1",
				DisplayName: "My VM Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "small-vm",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/my-vm",
			}
			_, err = catalogItemInstanceStore.Create(ctx, cii)
			Expect(err).ToNot(HaveOccurred())

			// Verify all were created
			retrievedST, err := serviceTypeStore.Get(ctx, "vm-service")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedST.ServiceType).To(Equal("vm"))

			retrievedCI, err := catalogItemStore.Get(ctx, "small-vm")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedCI.Spec.Resources[0].ServiceType).To(Equal("vm"))

			retrievedCII, err := catalogItemInstanceStore.Get(ctx, "my-vm")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedCII.Spec.CatalogItemId).To(Equal("small-vm"))
		})
	})

	Describe("Foreign Key Violation Detection", func() {
		It("should prevent creating CatalogItemInstance with non-existent CatalogItem", func() {
			ctx := context.Background()

			cii := model.CatalogItemInstance{
				ID:          "invalid-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Invalid Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "non-existent",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/invalid-cii",
			}
			_, err := catalogItemInstanceStore.Create(ctx, cii)
			Expect(err).To(Equal(store.ErrCatalogItemNotFoundRef))
		})

		It("should prevent updating CatalogItemInstance to non-existent CatalogItem", func() {
			ctx := context.Background()

			// Create valid hierarchy first
			st := model.ServiceType{
				ID:          "vm-st-update",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/vm-st-update",
			}
			_, err := serviceTypeStore.Create(ctx, st)
			Expect(err).ToNot(HaveOccurred())

			ci := model.CatalogItem{
				ID:          "test-ci-update",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Item",
				Spec:        testutil.ModelCatalogSpec("vm", []model.FieldConfiguration{}),
				Path:        "catalog-items/test-ci-update",
			}
			_, err = catalogItemStore.Create(ctx, ci)
			Expect(err).ToNot(HaveOccurred())

			cii := model.CatalogItemInstance{
				ID:          "test-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "test-ci-update",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/test-cii",
			}
			created, err := catalogItemInstanceStore.Create(ctx, cii)
			Expect(err).ToNot(HaveOccurred())

			// Try to update to non-existent catalog item
			created.Spec.CatalogItemId = "non-existent"
			_, err = catalogItemInstanceStore.Update(ctx, created)
			Expect(err).To(Equal(store.ErrCatalogItemNotFoundRef))
		})
	})

	Describe("Deletion Workflow", func() {
		It("should prevent deleting CatalogItem with existing instances", func() {
			ctx := context.Background()

			// Create full hierarchy
			st := model.ServiceType{
				ID:          "vm-st-del",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/vm-st-del",
			}
			_, err := serviceTypeStore.Create(ctx, st)
			Expect(err).ToNot(HaveOccurred())

			ci := model.CatalogItem{
				ID:          "test-ci-del",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Item",
				Spec:        testutil.ModelCatalogSpec("vm", []model.FieldConfiguration{}),
				Path:        "catalog-items/test-ci-del",
			}
			_, err = catalogItemStore.Create(ctx, ci)
			Expect(err).ToNot(HaveOccurred())

			cii := model.CatalogItemInstance{
				ID:          "test-cii-del",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Instance",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "test-ci-del",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/test-cii-del",
			}
			_, err = catalogItemInstanceStore.Create(ctx, cii)
			Expect(err).ToNot(HaveOccurred())

			// Try to delete catalog item with existing instance
			err = catalogItemStore.Delete(ctx, "test-ci-del")
			Expect(err).To(Equal(store.ErrCatalogItemHasInstances))

			// Delete instance first
			err = catalogItemInstanceStore.Delete(ctx, "test-cii-del")
			Expect(err).ToNot(HaveOccurred())

			// Now deletion should succeed
			err = catalogItemStore.Delete(ctx, "test-ci-del")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should allow deleting CatalogItem without instances", func() {
			ctx := context.Background()

			// Create ServiceType and CatalogItem only
			st := model.ServiceType{
				ID:          "vm-st-del-no-inst",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/vm-st-del-no-inst",
			}
			_, err := serviceTypeStore.Create(ctx, st)
			Expect(err).ToNot(HaveOccurred())

			ci := model.CatalogItem{
				ID:          "test-ci-del-no-inst",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Item",
				Spec:        testutil.ModelCatalogSpec("vm", []model.FieldConfiguration{}),
				Path:        "catalog-items/test-ci-del-no-inst",
			}
			_, err = catalogItemStore.Create(ctx, ci)
			Expect(err).ToNot(HaveOccurred())

			// Delete should succeed since there are no instances
			err = catalogItemStore.Delete(ctx, "test-ci-del-no-inst")
			Expect(err).ToNot(HaveOccurred())

			// Verify deletion
			_, err = catalogItemStore.Get(ctx, "test-ci-del-no-inst")
			Expect(err).To(Equal(store.ErrCatalogItemNotFound))
		})
	})

	Describe("Correct Error Returns", func() {
		It("should return correct error for each violation type", func() {
			ctx := context.Background()

			// Test ErrCatalogItemNotFoundRef
			cii := model.CatalogItemInstance{
				ID:          "err-test-2",
				ApiVersion:  "v1alpha1",
				DisplayName: "Error Test 2",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "missing-ci",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/err-test-2",
			}
			_, err := catalogItemInstanceStore.Create(ctx, cii)
			Expect(err).To(Equal(store.ErrCatalogItemNotFoundRef))

			// Test ErrCatalogItemHasInstances
			// First create valid hierarchy
			st := model.ServiceType{
				ID:          "vm-st-err",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/vm-st-err",
			}
			_, err = serviceTypeStore.Create(ctx, st)
			Expect(err).ToNot(HaveOccurred())

			ci2 := model.CatalogItem{
				ID:          "err-test-ci",
				ApiVersion:  "v1alpha1",
				DisplayName: "Error Test CI",
				Spec:        testutil.ModelCatalogSpec("vm", []model.FieldConfiguration{}),
				Path:        "catalog-items/err-test-ci",
			}
			_, err = catalogItemStore.Create(ctx, ci2)
			Expect(err).ToNot(HaveOccurred())

			cii2 := model.CatalogItemInstance{
				ID:          "err-test-cii",
				ApiVersion:  "v1alpha1",
				DisplayName: "Error Test CII",
				Spec: model.CatalogItemInstanceSpec{
					CatalogItemId: "err-test-ci",
					UserValues:    []model.UserValue{},
				},
				Path: "catalog-item-instances/err-test-cii",
			}
			_, err = catalogItemInstanceStore.Create(ctx, cii2)
			Expect(err).ToNot(HaveOccurred())

			// Now try to delete catalog item with instance
			err = catalogItemStore.Delete(ctx, "err-test-ci")
			Expect(err).To(Equal(store.ErrCatalogItemHasInstances))
		})
	})
})
