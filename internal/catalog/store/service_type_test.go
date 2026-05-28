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

var _ = Describe("ServiceType Store", func() {
	var (
		db               *gorm.DB
		serviceTypeStore store.ServiceTypeStore
	)

	BeforeEach(func() {
		// Create in-memory SQLite database
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())

		// Auto-migrate
		err = db.AutoMigrate(&model.ServiceType{})
		Expect(err).ToNot(HaveOccurred())

		serviceTypeStore = store.NewServiceTypeStore(db, slog.Default())
	})

	AfterEach(func() {
		sqlDB, err := db.DB()
		Expect(err).ToNot(HaveOccurred())
		_ = sqlDB.Close()
	})

	Describe("Create", func() {
		It("should create a new service type successfully", func() {
			st := model.ServiceType{
				ID:          "test-vm",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Metadata: model.Metadata{
					Labels: map[string]string{"env": "test"},
				},
				Spec: map[string]any{
					"vcpu": map[string]any{"count": 2},
				},
				Path: "service-types/test-vm",
			}

			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())

			// Verify it was created
			retrieved, err := serviceTypeStore.Get(context.Background(), "test-vm")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal("test-vm"))
			Expect(retrieved.ApiVersion).To(Equal("v1alpha1"))
			Expect(retrieved.ServiceType).To(Equal("vm"))
			Expect(retrieved.Metadata.Labels["env"]).To(Equal("test"))
		})

		It("should return error when creating duplicate ID", func() {
			st := model.ServiceType{
				ID:          "duplicate-id",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/duplicate-id",
			}

			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())

			// Try to create again with same ID
			st2 := model.ServiceType{
				ID:          "duplicate-id",
				ApiVersion:  "v1alpha1",
				ServiceType: "container",
				Spec:        map[string]any{},
				Path:        "service-types/duplicate-id",
			}

			_, err = serviceTypeStore.Create(context.Background(), st2)
			Expect(err).To(Equal(store.ErrServiceTypeIDTaken))
		})
		It("should return error when creating duplicate Service Type", func() {
			st := model.ServiceType{
				ID:          "duplicate-id",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/duplicate-id",
			}

			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())

			// Try to create again with same ID
			st2 := model.ServiceType{
				ID:          "duplicate-service-type",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{},
				Path:        "service-types/duplicate-service-type",
			}

			_, err = serviceTypeStore.Create(context.Background(), st2)
			Expect(err).To(Equal(store.ErrServiceTypeServiceTypeTaken))
		})
	})

	Describe("Get", func() {
		It("should retrieve an existing service type", func() {
			st := model.ServiceType{
				ID:          "get-test",
				ApiVersion:  "v1alpha1",
				ServiceType: "database",
				Spec:        map[string]any{"engine": "postgres"},
				Path:        "service-types/get-test",
			}

			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())

			retrieved, err := serviceTypeStore.Get(context.Background(), "get-test")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal("get-test"))
			Expect(retrieved.ServiceType).To(Equal("database"))
		})

		It("should return error for non-existent service type", func() {
			_, err := serviceTypeStore.Get(context.Background(), "non-existent")
			Expect(err).To(Equal(store.ErrServiceTypeNotFound))
		})
	})

	Describe("GetByServiceType", func() {
		It("should retrieve a service type by its service_type value", func() {
			st := model.ServiceType{
				ID:          "get-by-st",
				ApiVersion:  "v1alpha1",
				ServiceType: "vm",
				Spec:        map[string]any{"vcpu": map[string]any{"count": float64(2)}},
				Path:        "service-types/get-by-st",
			}
			_, err := serviceTypeStore.Create(context.Background(), st)
			Expect(err).ToNot(HaveOccurred())

			retrieved, err := serviceTypeStore.GetByServiceType(context.Background(), "vm")
			Expect(err).ToNot(HaveOccurred())
			Expect(retrieved.ID).To(Equal("get-by-st"))
			Expect(retrieved.ServiceType).To(Equal("vm"))
		})

		It("should return error for non-existent service_type", func() {
			_, err := serviceTypeStore.GetByServiceType(context.Background(), "nonexistent")
			Expect(err).To(Equal(store.ErrServiceTypeNotFound))
		})
	})

	Describe("List", func() {
		It("should return empty list when no service types exist", func() {
			results, err := serviceTypeStore.List(context.Background(), &store.ServiceTypeListOptions{
				PageSize: 100,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.ServiceTypes).To(BeEmpty())
			Expect(results.NextPageToken).To(BeNil())
		})

		It("should list all service types", func() {
			// Create multiple service types
			for i := 1; i <= 3; i++ {
				st := model.ServiceType{
					ID:          fmt.Sprintf("st-%d", i),
					ApiVersion:  "v1alpha1",
					ServiceType: fmt.Sprintf("vm-%d", i),
					Spec:        map[string]any{},
					Path:        fmt.Sprintf("service-types/st-%d", i),
				}
				// Add small delay to ensure different create times
				time.Sleep(time.Millisecond)
				_, err := serviceTypeStore.Create(context.Background(), st)
				Expect(err).ToNot(HaveOccurred())
			}

			results, err := serviceTypeStore.List(context.Background(), &store.ServiceTypeListOptions{
				PageSize: 100,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(results.ServiceTypes).To(HaveLen(3))
			Expect(results.NextPageToken).To(BeNil())
		})

		It("should handle pagination correctly", func() {
			// Create 6 service types
			for i := 1; i <= 6; i++ {
				st := model.ServiceType{
					ID:          fmt.Sprintf("page-st-%d", i),
					ApiVersion:  "v1alpha1",
					ServiceType: fmt.Sprintf("vm-%d", i),
					Spec:        map[string]any{},
					Path:        fmt.Sprintf("service-types/page-st-%d", i),
				}
				time.Sleep(time.Millisecond)
				_, err := serviceTypeStore.Create(context.Background(), st)
				Expect(err).ToNot(HaveOccurred())
			}

			var pageToken *string
			for _, pageSize := range []int{3, 2} {
				results, err := serviceTypeStore.List(context.Background(), &store.ServiceTypeListOptions{
					PageSize:  pageSize,
					PageToken: pageToken,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(results.ServiceTypes).To(HaveLen(pageSize))
				Expect(results.NextPageToken).ToNot(BeNil())
				pageToken = results.NextPageToken
			}

			// Get last page (should have 1 item)
			lastPageResults, err := serviceTypeStore.List(context.Background(), &store.ServiceTypeListOptions{
				PageToken: pageToken,
				PageSize:  4,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(lastPageResults.ServiceTypes).To(HaveLen(1))
			Expect(lastPageResults.NextPageToken).To(BeNil())
		})

		It("should reject invalid page_token", func() {
			badToken := "not-a-valid-token"
			_, err := serviceTypeStore.List(context.Background(), &store.ServiceTypeListOptions{
				PageToken: &badToken,
			})
			Expect(err).To(MatchError(store.ErrInvalidPageToken))
		})
	})
})
