package v1alpha1_test

import (
	"context"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	"github.com/dcm-project/control-plane/internal/catalog/config"
	v1alpha1 "github.com/dcm-project/control-plane/internal/catalog/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/placement"
	"github.com/dcm-project/control-plane/internal/catalog/service"
	"github.com/dcm-project/control-plane/internal/catalog/store"
	"github.com/dcm-project/control-plane/internal/catalog/store/model"
)

type noopPMClient struct{}

// Ensure noopPMClient implements placement.Client at compile time.
var _ placement.Client = (*noopPMClient)(nil)

func (n *noopPMClient) CreateResource(_ context.Context, _ placement.CreateResourceRequest, _ string) (*placement.Resource, error) {
	return &placement.Resource{}, nil
}

func (n *noopPMClient) DeleteResource(_ context.Context, _ string) error {
	return nil
}

func (n *noopPMClient) RehydrateResource(_ context.Context, _ string, newResourceID string) (*placement.Resource, error) {
	return &placement.Resource{ID: newResourceID}, nil
}

var _ = Describe("Health Handler", func() {
	var (
		handler   *v1alpha1.Handler
		db        *gorm.DB
		dataStore store.Store
	)

	BeforeEach(func() {
		// Create in-memory SQLite database for testing
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Discard,
		})
		Expect(err).ToNot(HaveOccurred())

		// Auto-migrate
		err = db.AutoMigrate(
			&model.ServiceType{},
			&model.CatalogItem{},
			&model.CatalogItemInstance{},
		)
		Expect(err).ToNot(HaveOccurred())

		dataStore = store.NewStore(db, slog.Default())
		svc, err := service.NewService(dataStore, &noopPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())
		handler = v1alpha1.NewHandler(svc, slog.Default())
	})

	AfterEach(func() {
		_ = dataStore.Close()
	})

	Describe("GetHealth", func() {
		It("should return ok status", func() {
			request := server.GetHealthRequestObject{}
			response, err := handler.GetHealth(context.Background(), request)

			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(BeAssignableToTypeOf(server.GetHealth200JSONResponse{}))

			healthResponse := response.(server.GetHealth200JSONResponse)
			Expect(healthResponse.Status).To(Equal("ok"))
			Expect(healthResponse.Path).ToNot(BeNil())
			Expect(*healthResponse.Path).To(Equal("/api/v1alpha1/health"))
		})
	})
})
