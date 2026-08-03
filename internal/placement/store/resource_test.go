package store_test

import (
	"context"
	"encoding/base64"

	"github.com/dcm-project/control-plane/internal/placement/store"
	"github.com/dcm-project/control-plane/internal/placement/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("Resource Store", func() {
	var (
		db           *gorm.DB
		requestStore store.Resource
		ctx          context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Resource{})).To(Succeed())

		requestStore = store.NewResource(db)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("Create", func() {
		It("persists the resource without optional fields", func() {
			provider := "test-provider"
			approval := "APPROVED"
			r := model.Resource{
				ID:                    uuid.New().String(),
				RunID:                 "run-1",
				Name:                  "main",
				CatalogItemInstanceId: "catalog-instance-123",
				Spec:                  map[string]any{"cpu": "2", "memory": "4Gi"},
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
				Path:                  "resources/" + uuid.New().String(),
			}
			created, err := requestStore.Create(ctx, r)

			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).To(Equal(r.ID))
			Expect(created.CatalogItemInstanceId).To(Equal("catalog-instance-123"))
			Expect(created.Spec).To(Equal(map[string]any{"cpu": "2", "memory": "4Gi"}))
			Expect(created.ProviderName).NotTo(BeNil())
			Expect(*created.ProviderName).To(Equal("test-provider"))
			Expect(created.ApprovalStatus).NotTo(BeNil())
			Expect(*created.ApprovalStatus).To(Equal("APPROVED"))
		})

		It("returns error for duplicate ID", func() {
			id := uuid.New().String()
			provider := "test-provider"
			approval := "APPROVED"
			r1 := model.Resource{
				ID:                    id,
				RunID:                 "run-1",
				Name:                  "main",
				CatalogItemInstanceId: "catalog-instance-123",
				Spec:                  map[string]any{"cpu": "2"},
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
				Path:                  "resources/" + id,
			}
			_, err := requestStore.Create(ctx, r1)
			Expect(err).NotTo(HaveOccurred())

			// Attempt to create another resource with same ID
			r2 := model.Resource{
				ID:                    id,
				RunID:                 "run-1",
				Name:                  "main",
				CatalogItemInstanceId: "catalog-instance-456",
				Spec:                  map[string]any{"cpu": "4"},
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
				Path:                  "resources/" + id,
			}
			_, err = requestStore.Create(ctx, r2)

			Expect(err).To(Equal(store.ErrResourceIdExist))
		})
	})

	Describe("CreateBatch", func() {
		It("persists multiple resources in one call", func() {
			provider := "test-provider"
			approval := "APPROVED"
			id1 := uuid.New().String()
			id2 := uuid.New().String()
			rows := []model.Resource{
				{
					ID:                    id1,
					RunID:                 "run-batch-1",
					Name:                  "db",
					CatalogItemInstanceId: "catalog-instance-123",
					Spec:                  map[string]any{"cpu": "2"},
					ProviderName:          &provider,
					ApprovalStatus:        &approval,
					Path:                  "resources/" + id1,
					DagLevel:              0,
				},
				{
					ID:                    id2,
					RunID:                 "run-batch-1",
					Name:                  "app",
					CatalogItemInstanceId: "catalog-instance-123",
					Spec:                  map[string]any{"cpu": "4"},
					RequiresResources:     []string{"db"},
					ProviderName:          &provider,
					ApprovalStatus:        &approval,
					Path:                  "resources/" + id2,
					DagLevel:              1,
				},
			}

			created, err := requestStore.CreateBatch(ctx, rows)
			Expect(err).NotTo(HaveOccurred())
			Expect(created).To(HaveLen(2))
			Expect(created[0].ID).To(Equal(id1))
			Expect(created[1].ID).To(Equal(id2))

			listed, err := requestStore.ListByRunID(ctx, "run-batch-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(listed).To(HaveLen(2))
		})

		It("returns error when any resource ID already exists", func() {
			provider := "test-provider"
			approval := "APPROVED"
			existingID := uuid.New().String()
			_, err := requestStore.Create(ctx, model.Resource{
				ID:                    existingID,
				RunID:                 "run-existing",
				Name:                  "main",
				CatalogItemInstanceId: "catalog-instance-123",
				Spec:                  map[string]any{"cpu": "2"},
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
				Path:                  "resources/" + existingID,
			})
			Expect(err).NotTo(HaveOccurred())

			newID := uuid.New().String()
			_, err = requestStore.CreateBatch(ctx, []model.Resource{
				{
					ID:                    newID,
					RunID:                 "run-batch-dup",
					Name:                  "a",
					CatalogItemInstanceId: "catalog-instance-123",
					Spec:                  map[string]any{},
					ProviderName:          &provider,
					ApprovalStatus:        &approval,
					Path:                  "resources/" + newID,
				},
				{
					ID:                    existingID,
					RunID:                 "run-batch-dup",
					Name:                  "b",
					CatalogItemInstanceId: "catalog-instance-123",
					Spec:                  map[string]any{},
					ProviderName:          &provider,
					ApprovalStatus:        &approval,
					Path:                  "resources/" + existingID,
				},
			})
			Expect(err).To(Equal(store.ErrResourceIdExist))
		})
	})

	Describe("Get", func() {
		It("retrieves by ID", func() {
			provider := "test-provider"
			approval := "APPROVED"
			r := model.Resource{
				ID:                    uuid.New().String(),
				RunID:                 "run-1",
				Name:                  "main",
				CatalogItemInstanceId: "catalog-instance-456",
				Spec:                  map[string]any{"test": "data"},
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
				Path:                  "resources/" + uuid.New().String(),
			}
			_, _ = requestStore.Create(ctx, r)

			found, err := requestStore.Get(ctx, r.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(found.CatalogItemInstanceId).To(Equal("catalog-instance-456"))
		})

		It("returns ErrResourceNotFound for missing ID", func() {
			_, err := requestStore.Get(ctx, uuid.New().String())

			Expect(err).To(Equal(store.ErrResourceNotFound))
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			provider1 := "provider-a"
			provider2 := "provider-b"
			approval := "APPROVED"
			// Create test data
			requests := []model.Resource{
				{ID: uuid.New().String(), ProviderName: &provider1, ApprovalStatus: &approval, CatalogItemInstanceId: "cat-1", Spec: map[string]any{}, Path: "resources/1"},
				{ID: uuid.New().String(), ProviderName: &provider2, ApprovalStatus: &approval, CatalogItemInstanceId: "cat-2", Spec: map[string]any{}, Path: "resources/2"},
				{ID: uuid.New().String(), ProviderName: &provider1, ApprovalStatus: &approval, CatalogItemInstanceId: "cat-3", Spec: map[string]any{}, Path: "resources/3"},
			}
			for _, r := range requests {
				_, err := requestStore.Create(ctx, r)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("returns all requests when opts is nil", func() {
			result, err := requestStore.List(ctx, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Resources).To(HaveLen(3))
			Expect(result.NextPageToken).To(BeNil())
		})

		It("filters by provider name", func() {
			providerName := "provider-a"
			opts := &store.ResourceListOptions{
				ProviderName: &providerName,
			}

			result, err := requestStore.List(ctx, opts)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Resources).To(HaveLen(2))
			for _, r := range result.Resources {
				Expect(*r.ProviderName).To(Equal("provider-a"))
			}
		})

		It("supports pagination with page size", func() {
			opts := &store.ResourceListOptions{
				PageSize: 2,
			}

			result, err := requestStore.List(ctx, opts)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Resources).To(HaveLen(2))
			Expect(result.NextPageToken).NotTo(BeNil())
		})

		It("supports pagination with page token", func() {
			// Get first page
			opts1 := &store.ResourceListOptions{
				PageSize: 2,
			}
			result1, err := requestStore.List(ctx, opts1)
			Expect(err).NotTo(HaveOccurred())
			Expect(result1.NextPageToken).NotTo(BeNil())

			// Get second page
			opts2 := &store.ResourceListOptions{
				PageSize:  2,
				PageToken: result1.NextPageToken,
			}
			result2, err := requestStore.List(ctx, opts2)

			Expect(err).NotTo(HaveOccurred())
			Expect(result2.Resources).To(HaveLen(1))
			Expect(result2.NextPageToken).To(BeNil())
		})

		It("defaults PageSize when zero or negative", func() {
			// PageSize=0 should fallback to default (100) and not error
			pageZero, err := requestStore.List(ctx, &store.ResourceListOptions{
				PageSize: 0,
			})
			Expect(err).NotTo(HaveOccurred())
			// We only created 3 items, so all should be returned with the default page size
			Expect(pageZero.Resources).To(HaveLen(3))
			Expect(pageZero.NextPageToken).To(BeNil())

			// Negative PageSize should also fallback to default (100)
			pageNegative, err := requestStore.List(ctx, &store.ResourceListOptions{
				PageSize: -1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(pageNegative.Resources).To(HaveLen(3))
			Expect(pageNegative.NextPageToken).To(BeNil())
		})

		It("treats malformed PageToken as starting from offset 0", func() {
			// Malformed, non-base64 token should be treated as offset 0
			pageToken := "!!not-base64!!"
			page, err := requestStore.List(ctx, &store.ResourceListOptions{
				PageToken: &pageToken,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(page.Resources).To(HaveLen(3))
			// Entire result set should be visible from offset 0

			// Well-formed base64 that does not decode to an integer should also be treated as offset 0
			malformedToken := base64.StdEncoding.EncodeToString([]byte("not-an-int"))
			page, err = requestStore.List(ctx, &store.ResourceListOptions{
				PageToken: &malformedToken,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(page.Resources).To(HaveLen(3))
		})
	})

	Describe("Delete", func() {
		It("deletes the resource", func() {
			provider := "test-provider"
			approval := "APPROVED"
			r := model.Resource{
				ID:                    uuid.New().String(),
				RunID:                 "run-1",
				Name:                  "main",
				CatalogItemInstanceId: "cat-del",
				Spec:                  map[string]any{},
				ProviderName:          &provider,
				ApprovalStatus:        &approval,
				Path:                  "resources/del",
			}
			_, _ = requestStore.Create(ctx, r)

			err := requestStore.Delete(ctx, r.ID)

			Expect(err).NotTo(HaveOccurred())

			_, err = requestStore.Get(ctx, r.ID)
			Expect(err).To(Equal(store.ErrResourceNotFound))
		})

		It("returns ErrResourceNotFound for missing ID", func() {
			err := requestStore.Delete(ctx, uuid.New().String())

			Expect(err).To(Equal(store.ErrResourceNotFound))
		})
	})

	Describe("UpdateStatusByRunID", func() {
		It("updates status for all resources in the run", func() {
			provider := "test-provider"
			approval := "APPROVED"
			id1 := uuid.New().String()
			id2 := uuid.New().String()
			_, err := requestStore.CreateBatch(ctx, []model.Resource{
				{
					ID:                    id1,
					RunID:                 "run-status-1",
					Name:                  "db",
					CatalogItemInstanceId: "cat-1",
					Spec:                  map[string]any{},
					ProviderName:          &provider,
					ApprovalStatus:        &approval,
					Path:                  "resources/" + id1,
					Status:                "PENDING",
					DagLevel:              0,
				},
				{
					ID:                    id2,
					RunID:                 "run-status-1",
					Name:                  "app",
					CatalogItemInstanceId: "cat-1",
					Spec:                  map[string]any{},
					ProviderName:          &provider,
					ApprovalStatus:        &approval,
					Path:                  "resources/" + id2,
					Status:                "PENDING",
					DagLevel:              1,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			err = requestStore.UpdateStatusByRunID(ctx, "run-status-1", "PENDING_DELETION")
			Expect(err).NotTo(HaveOccurred())

			listed, err := requestStore.ListByRunID(ctx, "run-status-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(listed).To(HaveLen(2))
			Expect(listed[0].Status).To(Equal("PENDING_DELETION"))
			Expect(listed[1].Status).To(Equal("PENDING_DELETION"))
		})

		It("returns ErrResourceNotFound when run has no resources", func() {
			err := requestStore.UpdateStatusByRunID(ctx, "missing-run", "PENDING_DELETION")
			Expect(err).To(Equal(store.ErrResourceNotFound))
		})
	})
})
