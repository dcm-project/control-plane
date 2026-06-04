package provider_test

import (
	"context"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/dcm-project/control-plane/internal/sp/store/provider"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("Provider Store", func() {
	var (
		db            *gorm.DB
		providerStore provider.Provider
		ctx           context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Provider{})).To(Succeed())

		providerStore = provider.NewProvider(db)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("Create", func() {
		It("persists the provider", func() {
			p := newProvider("create-test")
			created, err := providerStore.Create(ctx, p)

			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).To(Equal(p.ID))
			Expect(created.Name).To(Equal("create-test"))
			Expect(created.SchemaVersion).To(Equal("v1alpha1"))
		})

		It("rejects duplicate names", func() {
			p1 := newProvider("duplicate-name")
			_, err := providerStore.Create(ctx, p1)
			Expect(err).NotTo(HaveOccurred())

			p2 := newProvider("duplicate-name")
			_, err = providerStore.Create(ctx, p2)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Get", func() {
		It("retrieves by ID", func() {
			p := newProvider("get-test")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			found, err := providerStore.Get(ctx, p.ID)

			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("get-test"))
		})

		It("returns ErrProviderNotFound for missing ID", func() {
			_, err := providerStore.Get(ctx, uuid.New().String())

			Expect(err).To(Equal(provider.ErrProviderNotFound))
		})
	})

	Describe("GetByName", func() {
		It("retrieves by name", func() {
			p := newProvider("named-provider")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			found, err := providerStore.GetByName(ctx, "named-provider")

			Expect(err).NotTo(HaveOccurred())
			Expect(found.ID).To(Equal(p.ID))
		})

		It("returns ErrProviderNotFound for missing name", func() {
			_, err := providerStore.GetByName(ctx, "non-existent")

			Expect(err).To(Equal(provider.ErrProviderNotFound))
		})
	})

	Describe("List", func() {
		It("returns all providers when filter is nil", func() {
			_, err := providerStore.Create(ctx, newProvider("p1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("p2"))
			Expect(err).NotTo(HaveOccurred())

			providers, err := providerStore.List(ctx, nil, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(2))
		})

		It("filters by service type", func() {
			p1 := newProvider("vm-provider")
			p1.ServiceType = "vm"
			_, err := providerStore.Create(ctx, p1)
			Expect(err).NotTo(HaveOccurred())

			p2 := newProvider("container-provider")
			p2.ServiceType = "container"
			_, err = providerStore.Create(ctx, p2)
			Expect(err).NotTo(HaveOccurred())

			vmType := "vm"
			vms, err := providerStore.List(ctx, &provider.ProviderFilter{ServiceType: &vmType}, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Name).To(Equal("vm-provider"))
		})

		It("filters by name", func() {
			_, err := providerStore.Create(ctx, newProvider("find-me"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("not-me"))
			Expect(err).NotTo(HaveOccurred())

			name := "find-me"
			providers, err := providerStore.List(ctx, &provider.ProviderFilter{Name: &name}, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(1))
			Expect(providers[0].Name).To(Equal("find-me"))
		})

		It("filters by both name and service type", func() {
			p1 := newProvider("vm-one")
			p1.ServiceType = "vm"
			_, err := providerStore.Create(ctx, p1)
			Expect(err).NotTo(HaveOccurred())

			p2 := newProvider("vm-two")
			p2.ServiceType = "vm"
			_, err = providerStore.Create(ctx, p2)
			Expect(err).NotTo(HaveOccurred())

			name := "vm-one"
			vmType := "vm"
			providers, err := providerStore.List(ctx, &provider.ProviderFilter{Name: &name, ServiceType: &vmType}, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(1))
			Expect(providers[0].Name).To(Equal("vm-one"))
		})

		It("respects pagination limit", func() {
			_, err := providerStore.Create(ctx, newProvider("page-p1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("page-p2"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("page-p3"))
			Expect(err).NotTo(HaveOccurred())

			providers, err := providerStore.List(ctx, nil, &provider.Pagination{Limit: 2, Offset: 0})

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(2))
		})

		It("respects pagination offset", func() {
			_, err := providerStore.Create(ctx, newProvider("offset-p1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("offset-p2"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("offset-p3"))
			Expect(err).NotTo(HaveOccurred())

			providers, err := providerStore.List(ctx, nil, &provider.Pagination{Limit: 10, Offset: 2})

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(1))
		})
	})

	Describe("Count", func() {
		It("returns total count without filter", func() {
			_, err := providerStore.Create(ctx, newProvider("count-p1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = providerStore.Create(ctx, newProvider("count-p2"))
			Expect(err).NotTo(HaveOccurred())

			count, err := providerStore.Count(ctx, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(2)))
		})

		It("returns filtered count", func() {
			p1 := newProvider("count-vm")
			p1.ServiceType = "vm"
			_, err := providerStore.Create(ctx, p1)
			Expect(err).NotTo(HaveOccurred())

			p2 := newProvider("count-container")
			p2.ServiceType = "container"
			_, err = providerStore.Create(ctx, p2)
			Expect(err).NotTo(HaveOccurred())

			vmType := "vm"
			count, err := providerStore.Count(ctx, &provider.ProviderFilter{ServiceType: &vmType})

			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(int64(1)))
		})
	})

	Describe("Delete", func() {
		It("removes the provider", func() {
			p := newProvider("to-delete")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			err = providerStore.Delete(ctx, p.ID)

			Expect(err).NotTo(HaveOccurred())
		})

		It("returns ErrProviderNotFound for missing ID", func() {
			err := providerStore.Delete(ctx, uuid.New().String())

			Expect(err).To(Equal(provider.ErrProviderNotFound))
		})
	})

	Describe("Update", func() {
		It("modifies existing provider", func() {
			p := newProvider("to-update")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			p.Endpoint = "https://new-endpoint.com"
			updated, err := providerStore.Update(ctx, p)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Endpoint).To(Equal("https://new-endpoint.com"))
		})

		It("returns ErrProviderNotFound for non-existing provider", func() {
			p := newProvider("non-existing")
			_, err := providerStore.Update(ctx, p)

			Expect(err).To(Equal(provider.ErrProviderNotFound))
		})
	})

	Describe("ListProvidersForHealthCheck", func() {
		It("returns providers with null next_health_check", func() {
			p := newProvider("null-next-check")
			p.NextHealthCheck = nil
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			providers, err := providerStore.ListProvidersForHealthCheck(ctx, now)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(1))
			Expect(providers[0].Name).To(Equal("null-next-check"))
		})

		It("returns providers with next_health_check in the past", func() {
			p := newProvider("past-check")
			pastTime := time.Now().Add(-1 * time.Hour)
			p.NextHealthCheck = &pastTime
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			providers, err := providerStore.ListProvidersForHealthCheck(ctx, now)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(1))
			Expect(providers[0].Name).To(Equal("past-check"))
		})

		It("returns providers with next_health_check equal to now", func() {
			now := time.Now()
			p := newProvider("equal-check")
			p.NextHealthCheck = &now
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			providers, err := providerStore.ListProvidersForHealthCheck(ctx, now)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(1))
			Expect(providers[0].Name).To(Equal("equal-check"))
		})

		It("excludes providers with next_health_check in the future", func() {
			p := newProvider("future-check")
			futureTime := time.Now().Add(1 * time.Hour)
			p.NextHealthCheck = &futureTime
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			providers, err := providerStore.ListProvidersForHealthCheck(ctx, now)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(BeEmpty())
		})

		It("returns empty list when no providers are due", func() {
			p := newProvider("not-due")
			futureTime := time.Now().Add(24 * time.Hour)
			p.NextHealthCheck = &futureTime
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			providers, err := providerStore.ListProvidersForHealthCheck(ctx, now)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(BeEmpty())
		})

		It("returns multiple providers due for health check", func() {
			p1 := newProvider("due-1")
			p1.NextHealthCheck = nil
			_, err := providerStore.Create(ctx, p1)
			Expect(err).NotTo(HaveOccurred())

			p2 := newProvider("due-2")
			pastTime := time.Now().Add(-30 * time.Minute)
			p2.NextHealthCheck = &pastTime
			_, err = providerStore.Create(ctx, p2)
			Expect(err).NotTo(HaveOccurred())

			p3 := newProvider("not-due")
			futureTime := time.Now().Add(1 * time.Hour)
			p3.NextHealthCheck = &futureTime
			_, err = providerStore.Create(ctx, p3)
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			providers, err := providerStore.ListProvidersForHealthCheck(ctx, now)

			Expect(err).NotTo(HaveOccurred())
			Expect(providers).To(HaveLen(2))
		})
	})

	Describe("UpdateHealthStatus", func() {
		It("updates health status to not_ready", func() {
			p := newProvider("health-update")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			nextCheck := time.Now().Add(1 * time.Hour)
			err = providerStore.UpdateHealthStatus(ctx, p.ID, model.HealthStatusUnavailable, 3, nextCheck)

			Expect(err).NotTo(HaveOccurred())

			updated, err := providerStore.Get(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.HealthStatus).To(Equal(model.HealthStatusUnavailable))
			Expect(updated.ConsecutiveFailures).To(Equal(3))
			Expect(updated.NextHealthCheck).NotTo(BeNil())
		})

		It("updates health status to ready", func() {
			p := newProvider("health-ready")
			p.HealthStatus = model.HealthStatusUnavailable
			p.ConsecutiveFailures = 5
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			nextCheck := time.Now().Add(10 * time.Second)
			err = providerStore.UpdateHealthStatus(ctx, p.ID, model.HealthStatusReady, 0, nextCheck)

			Expect(err).NotTo(HaveOccurred())

			updated, err := providerStore.Get(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.HealthStatus).To(Equal(model.HealthStatusReady))
			Expect(updated.ConsecutiveFailures).To(Equal(0))
		})

		It("updates consecutive failures count", func() {
			p := newProvider("failure-count")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			nextCheck := time.Now().Add(30 * time.Second)
			err = providerStore.UpdateHealthStatus(ctx, p.ID, model.HealthStatusReady, 2, nextCheck)

			Expect(err).NotTo(HaveOccurred())

			updated, err := providerStore.Get(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.ConsecutiveFailures).To(Equal(2))
		})

		It("updates next health check time", func() {
			p := newProvider("next-check-update")
			_, err := providerStore.Create(ctx, p)
			Expect(err).NotTo(HaveOccurred())

			nextCheck := time.Now().Add(5 * time.Minute)
			err = providerStore.UpdateHealthStatus(ctx, p.ID, model.HealthStatusReady, 0, nextCheck)

			Expect(err).NotTo(HaveOccurred())

			updated, err := providerStore.Get(ctx, p.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.NextHealthCheck).NotTo(BeNil())
			Expect(updated.NextHealthCheck.Unix()).To(Equal(nextCheck.Unix()))
		})

		It("returns ErrProviderNotFound for missing ID", func() {
			nextCheck := time.Now().Add(1 * time.Hour)
			err := providerStore.UpdateHealthStatus(ctx, uuid.New().String(), model.HealthStatusReady, 0, nextCheck)

			Expect(err).To(Equal(provider.ErrProviderNotFound))
		})
	})
})

func newProvider(name string) model.Provider {
	return model.Provider{
		ID:            uuid.New().String(),
		Name:          name,
		ServiceType:   "vm",
		SchemaVersion: "v1alpha1",
		Endpoint:      "https://example.com/api",
	}
}
