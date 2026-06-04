package cleanup_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/dcm-project/control-plane/internal/sp/cleanup"
	"github.com/dcm-project/control-plane/internal/sp/config"
	rmsvc "github.com/dcm-project/control-plane/internal/sp/service/resource_manager"
	"github.com/dcm-project/control-plane/internal/sp/store"
	"github.com/dcm-project/control-plane/internal/sp/store/model"
	"github.com/dcm-project/control-plane/internal/sp/testutil"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("Scheduler", func() {
	var (
		db              *gorm.DB
		dataStore       store.Store
		instanceService *rmsvc.InstanceService
		scheduler       *cleanup.Scheduler
		ctx             context.Context
		mockProvider    *httptest.Server
		providerName    string
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Provider{}, &model.ServiceTypeInstance{})).To(Succeed())

		providerName = "test-provider"

		// Mock provider server that returns 204 on DELETE
		mockProvider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))

		// Create a provider in the database
		provider := model.Provider{
			ID:            uuid.New().String(),
			Name:          providerName,
			ServiceType:   "vm",
			SchemaVersion: "v1",
			Endpoint:      mockProvider.URL,
			HealthStatus:  model.HealthStatusReady,
		}
		Expect(db.Create(&provider).Error).NotTo(HaveOccurred())

		dataStore = store.NewStore(db, store.WithServiceTypeInstanceRetry(testutil.FastServiceTypeInstanceRetry()...))
		instanceService = rmsvc.NewInstanceService(dataStore, resty.New().
			SetTimeout(5*time.Second).
			SetRetryCount(0))

		cfg := &config.CleanupConfig{
			Interval:   1 * time.Minute,
			MaxRetries: 3,
		}
		scheduler = cleanup.NewScheduler(dataStore, instanceService, cfg)
		ctx = context.Background()
	})

	AfterEach(func() {
		mockProvider.Close()
		sqlDB, err := db.DB()
		Expect(err).NotTo(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})

	Describe("ProcessPendingDeletions with provider health check", func() {
		It("skips instance when provider is not ready", func() {
			// Mark provider as NotReady
			Expect(db.Model(&model.Provider{}).Where("name = ?", providerName).
				Update("health_status", model.HealthStatusUnavailable).Error).NotTo(HaveOccurred())

			// Create an instance marked for deletion
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ProviderName: providerName,
				Status:       "PROVISIONING",
				InstanceName: "skip-inst",
				Spec:         map[string]any{"cpu": 1},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			// Instance should be marked as PENDING_PROVIDER, not deleted
			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("PENDING_PROVIDER"))
		})

		It("skips instance when provider is unhealthy", func() {
			Expect(db.Model(&model.Provider{}).Where("name = ?", providerName).
				Update("health_status", model.HealthStatusUnhealthy).Error).NotTo(HaveOccurred())

			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ProviderName: providerName,
				Status:       "PROVISIONING",
				InstanceName: "skip-unhealthy-inst",
				Spec:         map[string]any{"cpu": 1},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			found, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(*found.DeletionStatus).To(Equal("PENDING_PROVIDER"))
		})

		It("proceeds with deletion when provider is ready", func() {
			// Provider is already Ready from setup

			// Create an instance marked for deletion
			inst := model.ServiceTypeInstance{
				ID:           uuid.New().String(),
				ProviderName: providerName,
				Status:       "PROVISIONING",
				InstanceName: "delete-inst",
				Spec:         map[string]any{"cpu": 1},
			}
			Expect(db.Create(&inst).Error).NotTo(HaveOccurred())
			Expect(dataStore.ServiceTypeInstance().MarkForDeletion(ctx, inst.ID)).To(Succeed())

			scheduler.ProcessPendingDeletions(ctx)

			// Instance should be hard-deleted
			_, err := dataStore.ServiceTypeInstance().Get(ctx, inst.ID, true)
			Expect(err).To(HaveOccurred())
		})
	})
})
