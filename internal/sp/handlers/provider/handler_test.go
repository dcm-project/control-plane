package provider_test

import (
	"context"

	providerserver "github.com/dcm-project/control-plane/internal/sp/api/provider"
	providerhandler "github.com/dcm-project/control-plane/internal/sp/handlers/provider"
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

var _ = Describe("Handler", func() {
	var (
		db      *gorm.DB
		handler *providerhandler.Handler
		ctx     context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.Provider{})).To(Succeed())

		dataStore := store.NewStore(db)
		providerService := providersvc.NewProviderService(dataStore)
		handler = providerhandler.NewHandler(providerService)
		ctx = context.Background()
	})

	AfterEach(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	Describe("GetHealth", func() {
		It("returns ok", func() {
			resp, err := handler.GetHealth(ctx, providerserver.GetHealthRequestObject{})

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(providerserver.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Status).To(Equal("ok"))
		})
	})

	Describe("CreateProvider", func() {
		It("creates and returns 201", func() {
			req := providerserver.CreateProviderRequestObject{
				Body: &providerserver.Provider{
					Name:          "test-provider",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}

			resp, err := handler.CreateProvider(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(providerserver.CreateProvider201JSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 200 for idempotent re-registration", func() {
			req := providerserver.CreateProviderRequestObject{
				Body: &providerserver.Provider{
					Name:          "idempotent-provider",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}

			// First call creates
			resp1, err := handler.CreateProvider(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			_, ok := resp1.(providerserver.CreateProvider201JSONResponse)
			Expect(ok).To(BeTrue())

			// Second call updates (same name, no ID)
			resp2, err := handler.CreateProvider(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			_, ok = resp2.(providerserver.CreateProvider200JSONResponse)
			Expect(ok).To(BeTrue())
		})

		It("returns 409 for name conflict with different ID", func() {
			// Create first provider
			req1 := providerserver.CreateProviderRequestObject{
				Body: &providerserver.Provider{
					Name:          "conflict-name",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}
			_, err := handler.CreateProvider(ctx, req1)
			Expect(err).NotTo(HaveOccurred())

			// Try to create with same name but different ID
			differentID := uuid.New().String()
			req2 := providerserver.CreateProviderRequestObject{
				Params: providerserver.CreateProviderParams{Id: &differentID},
				Body: &providerserver.Provider{
					Name:          "conflict-name",
					Endpoint:      "https://other.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}

			resp, err := handler.CreateProvider(ctx, req2)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(providerserver.CreateProvider409ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("ListProviders", func() {
		It("returns empty list initially", func() {
			req := providerserver.ListProvidersRequestObject{}

			resp, err := handler.ListProviders(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(providerserver.ListProviders200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Providers).To(BeEmpty())
		})

		It("returns providers", func() {
			// Create providers first
			for _, name := range []string{"provider-1", "provider-2"} {
				createReq := providerserver.CreateProviderRequestObject{
					Body: &providerserver.Provider{
						Name:          name,
						Endpoint:      "https://example.com",
						ServiceType:   "vm",
						SchemaVersion: "v1alpha1",
					},
				}
				_, err := handler.CreateProvider(ctx, createReq)
				Expect(err).NotTo(HaveOccurred())
			}

			resp, err := handler.ListProviders(ctx, providerserver.ListProvidersRequestObject{})

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(providerserver.ListProviders200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(*jsonResp.Providers).To(HaveLen(2))
		})
	})

	Describe("GetProvider", func() {
		It("returns provider", func() {
			// Create a provider first
			createReq := providerserver.CreateProviderRequestObject{
				Body: &providerserver.Provider{
					Name:          "get-me",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}
			createResp, _ := handler.CreateProvider(ctx, createReq)
			created := createResp.(providerserver.CreateProvider201JSONResponse)

			req := providerserver.GetProviderRequestObject{
				ProviderId: *created.Id,
			}

			resp, err := handler.GetProvider(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(providerserver.GetProvider200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.Name).To(Equal("get-me"))
		})

		It("returns 404 for non-existent provider", func() {
			req := providerserver.GetProviderRequestObject{
				ProviderId: uuid.New().String(),
			}

			resp, err := handler.GetProvider(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(providerserver.GetProvider404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("ApplyProvider", func() {
		It("updates existing provider", func() {
			// Create a provider first
			createReq := providerserver.CreateProviderRequestObject{
				Body: &providerserver.Provider{
					Name:          "to-update",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}
			createResp, _ := handler.CreateProvider(ctx, createReq)
			created := createResp.(providerserver.CreateProvider201JSONResponse)

			// Update it
			updateReq := providerserver.ApplyProviderRequestObject{
				ProviderId: *created.Id,
				Body: &providerserver.Provider{
					Id:            created.Id,
					Name:          "to-update",
					Endpoint:      "https://updated.example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}

			resp, err := handler.ApplyProvider(ctx, updateReq)

			Expect(err).NotTo(HaveOccurred())
			jsonResp, ok := resp.(providerserver.ApplyProvider200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(jsonResp.Endpoint).To(Equal("https://updated.example.com"))
		})

		It("returns 404 for non-existent provider", func() {
			req := providerserver.ApplyProviderRequestObject{
				ProviderId: uuid.New().String(),
				Body: &providerserver.Provider{
					Name:          "test",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}

			resp, err := handler.ApplyProvider(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(providerserver.ApplyProvider404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("DeleteProvider", func() {
		It("deletes provider and returns 204", func() {
			// Create a provider first
			createReq := providerserver.CreateProviderRequestObject{
				Body: &providerserver.Provider{
					Name:          "to-delete",
					Endpoint:      "https://example.com",
					ServiceType:   "vm",
					SchemaVersion: "v1alpha1",
				},
			}
			createResp, _ := handler.CreateProvider(ctx, createReq)
			created := createResp.(providerserver.CreateProvider201JSONResponse)

			req := providerserver.DeleteProviderRequestObject{
				ProviderId: *created.Id,
			}

			resp, err := handler.DeleteProvider(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(providerserver.DeleteProvider204Response)
			Expect(ok).To(BeTrue())
		})

		It("returns 404 for non-existent provider", func() {
			req := providerserver.DeleteProviderRequestObject{
				ProviderId: uuid.New().String(),
			}

			resp, err := handler.DeleteProvider(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			_, ok := resp.(providerserver.DeleteProvider404ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue())
		})
	})
})
