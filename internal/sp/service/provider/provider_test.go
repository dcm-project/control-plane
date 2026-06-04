package provider_test

import (
	"context"
	"fmt"
	"time"

	providerserver "github.com/dcm-project/control-plane/internal/sp/api/provider"
	"github.com/dcm-project/control-plane/internal/sp/service"
	providersvc "github.com/dcm-project/control-plane/internal/sp/service/provider"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("ProviderService", func() {
	var (
		db              *gorm.DB
		dataStore       store.Store
		providerService *providersvc.ProviderService
		ctx             context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Provider{})).To(Succeed())

		dataStore = store.NewStore(db)
		providerService = providersvc.NewProviderService(dataStore)
		ctx = context.Background()
	})

	AfterEach(func() {
		_ = dataStore.Close()
	})

	Describe("RegisterOrUpdateProvider", func() {
		It("creates a new provider", func() {
			req := newProvider("new-provider")

			resp, updated, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeFalse())
			Expect(resp.Name).To(Equal("new-provider"))
		})

		It("updates existing provider with same name and ID", func() {
			req := newProvider("update-test")
			resp1, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			// Re-register with same ID
			req.Id = resp1.Id
			req.Endpoint = "https://updated.example.com"
			_, updated, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
		})

		It("updates existing provider with same name and no ID (idempotent)", func() {
			req := newProvider("idempotent-test")
			resp1, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			// Re-register with same name but NO ID
			req2 := newProvider("idempotent-test")
			req2.Endpoint = "https://updated.example.com"
			resp2, updated, err := providerService.RegisterOrUpdateProvider(ctx, req2, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
			Expect(*resp2.Id).To(Equal(*resp1.Id)) // Same ID returned
			Expect(resp2.Endpoint).To(Equal("https://updated.example.com"))
		})

		It("persists display_name on create and get", func() {
			dn := "Human-readable name"
			req := newProvider("persist-display-name")
			req.DisplayName = &dn

			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.DisplayName).NotTo(BeNil())
			Expect(*resp.DisplayName).To(Equal(dn))

			got, err := providerService.GetProvider(ctx, *resp.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.DisplayName).NotTo(BeNil())
			Expect(*got.DisplayName).To(Equal(dn))
		})

		It("persists operations on create and get", func() {
			ops := []string{"CREATE", "DELETE", "READ"}
			req := newProvider("persist-operations")
			req.Operations = &ops

			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Operations).NotTo(BeNil())
			Expect(*resp.Operations).To(Equal(ops))

			got, err := providerService.GetProvider(ctx, *resp.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Operations).NotTo(BeNil())
			Expect(*got.Operations).To(Equal(ops))
		})

		It("persists metadata on create and get", func() {
			region := "us-east-1"
			req := newProvider("persist-metadata")
			req.Metadata = &providerserver.ProviderMetadata{RegionCode: &region}

			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Metadata).NotTo(BeNil())
			Expect(resp.Metadata.RegionCode).NotTo(BeNil())
			Expect(*resp.Metadata.RegionCode).To(Equal(region))

			got, err := providerService.GetProvider(ctx, *resp.Id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Metadata).NotTo(BeNil())
			Expect(*got.Metadata.RegionCode).To(Equal(region))
		})

		It("updates metadata on re-register", func() {
			req := newProvider("meta-re-register")
			req.Metadata = &providerserver.ProviderMetadata{}
			req.Metadata.Set("supportedPlatforms", "baremetal")

			resp1, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())
			val, ok := resp1.Metadata.Get("supportedPlatforms")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("baremetal"))

			req2 := newProvider("meta-re-register")
			req2.Id = resp1.Id
			req2.Metadata = &providerserver.ProviderMetadata{}
			req2.Metadata.Set("supportedPlatforms", "kubevirt")

			resp2, updated, err := providerService.RegisterOrUpdateProvider(ctx, req2, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
			val2, ok := resp2.Metadata.Get("supportedPlatforms")
			Expect(ok).To(BeTrue())
			Expect(val2).To(Equal("kubevirt"))

			got, err := providerService.GetProvider(ctx, *resp1.Id)
			Expect(err).NotTo(HaveOccurred())
			val3, ok := got.Metadata.Get("supportedPlatforms")
			Expect(ok).To(BeTrue())
			Expect(val3).To(Equal("kubevirt"))
		})

		It("resets health backoff on re-register", func() {
			req := newProvider("health-reset")
			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			futureCheck := time.Now().Add(24 * time.Hour)
			err = db.Model(&model.Provider{}).Where("id = ?", *resp.Id).Updates(map[string]any{
				"health_status":        model.HealthStatusUnavailable,
				"consecutive_failures": 5,
				"next_health_check":    futureCheck,
			}).Error
			Expect(err).NotTo(HaveOccurred())

			req2 := newProvider("health-reset")
			req2.Id = resp.Id
			_, updated, err := providerService.RegisterOrUpdateProvider(ctx, req2, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())

			var p model.Provider
			Expect(db.Where("id = ?", *resp.Id).First(&p).Error).NotTo(HaveOccurred())
			Expect(p.ConsecutiveFailures).To(Equal(0))
			Expect(p.NextHealthCheck).To(BeNil())
			Expect(p.HealthStatus).To(Equal(model.HealthStatusUnavailable))
		})

		It("returns conflict when name exists with different ID", func() {
			req := newProvider("conflict-name")
			_, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			// Try with different ID
			newID := uuid.New().String()
			req.Id = &newID
			_, _, err = providerService.RegisterOrUpdateProvider(ctx, req, nil)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))
		})

		It("returns conflict when providerID exists with different name", func() {
			req := newProvider("first-name")
			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			// Try with same ID but different name
			req2 := newProvider("second-name")
			req2.Id = resp.Id
			_, _, err = providerService.RegisterOrUpdateProvider(ctx, req2, nil)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))
		})
	})

	Describe("GetProvider", func() {
		It("returns the provider", func() {
			req := newProvider("get-test")
			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			provider, err := providerService.GetProvider(ctx, *resp.Id)

			Expect(err).NotTo(HaveOccurred())
			Expect(provider.Name).To(Equal("get-test"))
		})

		It("returns error for non-existent provider", func() {
			_, err := providerService.GetProvider(ctx, uuid.New().String())

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})
	})

	Describe("ListProviders", func() {
		It("returns all providers", func() {
			_, _, err := providerService.RegisterOrUpdateProvider(ctx, newProvider("p1"), nil)
			Expect(err).NotTo(HaveOccurred())
			_, _, err = providerService.RegisterOrUpdateProvider(ctx, newProvider("p2"), nil)
			Expect(err).NotTo(HaveOccurred())

			result, err := providerService.ListProviders(ctx, "", 0, "")

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Providers).To(HaveLen(2))
		})

		It("filters by service type", func() {
			req1 := newProvider("vm-provider")
			req1.ServiceType = "vm"
			_, _, err := providerService.RegisterOrUpdateProvider(ctx, req1, nil)
			Expect(err).NotTo(HaveOccurred())

			req2 := newProvider("container-provider")
			req2.ServiceType = "container"
			_, _, err = providerService.RegisterOrUpdateProvider(ctx, req2, nil)
			Expect(err).NotTo(HaveOccurred())

			result, err := providerService.ListProviders(ctx, "vm", 0, "")

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Providers).To(HaveLen(1))
		})

		It("returns error for negative page size", func() {
			_, err := providerService.ListProviders(ctx, "", -1, "")

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})

		It("coerces page size to max", func() {
			var err error
			for i := 0; i < 5; i++ {
				_, _, err = providerService.RegisterOrUpdateProvider(ctx, newProvider(fmt.Sprintf("coerce-p%d", i)), nil)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := providerService.ListProviders(ctx, "", 2, "")

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Providers).To(HaveLen(2))
			Expect(result.NextPageToken).NotTo(BeEmpty())
		})

		It("paginates through results", func() {
			var err error
			for i := 0; i < 5; i++ {
				_, _, err = providerService.RegisterOrUpdateProvider(ctx, newProvider(fmt.Sprintf("paginate-p%d", i)), nil)
				Expect(err).NotTo(HaveOccurred())
			}

			// First page
			result1, err := providerService.ListProviders(ctx, "", 2, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result1.Providers).To(HaveLen(2))
			Expect(result1.NextPageToken).NotTo(BeEmpty())

			// Second page
			result2, err := providerService.ListProviders(ctx, "", 2, result1.NextPageToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(result2.Providers).To(HaveLen(2))
			Expect(result2.NextPageToken).NotTo(BeEmpty())

			// Third page (last)
			result3, err := providerService.ListProviders(ctx, "", 2, result2.NextPageToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(result3.Providers).To(HaveLen(1))
			Expect(result3.NextPageToken).To(BeEmpty())
		})

		It("returns error for invalid page token", func() {
			_, err := providerService.ListProviders(ctx, "", 0, "invalid-token")

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeValidation))
		})
	})

	Describe("UpdateProvider", func() {
		It("updates the provider", func() {
			req := newProvider("update-provider")
			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			update := &providerserver.Provider{
				Id:            resp.Id,
				Name:          "update-provider",
				Endpoint:      "https://updated.example.com",
				ServiceType:   "vm",
				SchemaVersion: "v1alpha1",
			}

			updated, err := providerService.UpdateProvider(ctx, *resp.Id, update)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Endpoint).To(Equal("https://updated.example.com"))
		})

		It("preserves health backoff on field update", func() {
			req := newProvider("health-preserve")
			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			futureCheck := time.Now().Add(24 * time.Hour)
			err = db.Model(&model.Provider{}).Where("id = ?", *resp.Id).Updates(map[string]any{
				"health_status":        model.HealthStatusUnavailable,
				"consecutive_failures": 5,
				"next_health_check":    futureCheck,
			}).Error
			Expect(err).NotTo(HaveOccurred())

			update := &providerserver.Provider{
				Name:          "health-preserve",
				Endpoint:      "https://updated.example.com",
				ServiceType:   "vm",
				SchemaVersion: "v1alpha1",
			}
			_, err = providerService.UpdateProvider(ctx, *resp.Id, update)
			Expect(err).NotTo(HaveOccurred())

			var p model.Provider
			Expect(db.Where("id = ?", *resp.Id).First(&p).Error).NotTo(HaveOccurred())
			Expect(p.ConsecutiveFailures).To(Equal(5))
			Expect(p.NextHealthCheck).NotTo(BeNil())
			Expect(p.HealthStatus).To(Equal(model.HealthStatusUnavailable))
		})

		It("returns conflict when renaming to existing name", func() {
			// Create two providers
			_, _, err := providerService.RegisterOrUpdateProvider(ctx, newProvider("original-name"), nil)
			Expect(err).NotTo(HaveOccurred())
			var resp2 *providerserver.Provider
			resp2, _, err = providerService.RegisterOrUpdateProvider(ctx, newProvider("to-rename"), nil)
			Expect(err).NotTo(HaveOccurred())

			// Try to rename second provider to first provider's name
			update := &providerserver.Provider{
				Name:          "original-name",
				Endpoint:      "https://example.com",
				ServiceType:   "vm",
				SchemaVersion: "v1alpha1",
			}

			_, err = providerService.UpdateProvider(ctx, *resp2.Id, update)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeConflict))
		})

		It("returns error for non-existent provider", func() {
			update := &providerserver.Provider{
				Name:          "test",
				Endpoint:      "https://example.com",
				ServiceType:   "vm",
				SchemaVersion: "v1alpha1",
			}

			_, err := providerService.UpdateProvider(ctx, uuid.New().String(), update)

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})
	})

	Describe("DeleteProvider", func() {
		It("deletes the provider", func() {
			req := newProvider("to-delete")
			resp, _, err := providerService.RegisterOrUpdateProvider(ctx, req, nil)
			Expect(err).NotTo(HaveOccurred())

			err = providerService.DeleteProvider(ctx, *resp.Id)

			Expect(err).NotTo(HaveOccurred())
		})

		It("returns error for non-existent provider", func() {
			err := providerService.DeleteProvider(ctx, uuid.New().String())

			Expect(err).To(HaveOccurred())
			svcErr, ok := err.(*service.ServiceError)
			Expect(ok).To(BeTrue())
			Expect(svcErr.Code).To(Equal(service.ErrCodeNotFound))
		})
	})
})

func newProvider(name string) *providerserver.Provider {
	return &providerserver.Provider{
		Name:          name,
		Endpoint:      "https://example.com/api",
		ServiceType:   "vm",
		SchemaVersion: "v1alpha1",
	}
}
