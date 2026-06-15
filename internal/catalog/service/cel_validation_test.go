package service_test

import (
	"context"
	"errors"
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

func serviceTypeSpecWithOutputs(base map[string]any, outputs map[string]any) map[string]any {
	spec := make(map[string]any, len(base)+1)
	for k, v := range base {
		spec[k] = v
	}
	spec["outputs"] = outputs
	return spec
}

func devAppCatalogItemSpecWithCEL() v1alpha1.CatalogItemSpec {
	requiresOrdersDb := []string{"ordersDb"}
	return v1alpha1.CatalogItemSpec{
		Resources: []v1alpha1.CatalogResource{
			{
				Name:        "ordersDb",
				ServiceType: "database",
				Fields: &[]v1alpha1.FieldConfiguration{
					{Path: "engine", Default: "postgres"},
				},
			},
			{
				Name:              "app",
				ServiceType:       "container",
				RequiresResources: &requiresOrdersDb,
				Fields: &[]v1alpha1.FieldConfiguration{
					{Path: "database_url", Default: "${ordersDb.connectionString}"},
				},
			},
		},
	}
}

var _ = Describe("CEL validation", func() {
	var (
		ctx context.Context
		db  *gorm.DB
		str store.Store
		svc service.Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
		Expect(err).ToNot(HaveOccurred())
		Expect(db.Exec("PRAGMA foreign_keys = ON").Error).To(Succeed())
		Expect(db.AutoMigrate(&model.ServiceType{}, &model.CatalogItem{}, &model.CatalogItemInstance{})).To(Succeed())
		str = store.NewStore(db, slog.Default())
		svc, err = service.NewService(str, &mockPMClient{}, config.DefaultSeedConfig(), slog.Default())
		Expect(err).ToNot(HaveOccurred())

		ensureServiceTypeWithSpec(ctx, str, "db-cel", "database", serviceTypeSpecWithOutputs(
			map[string]any{"engine": "postgres"},
			map[string]any{"connectionString": map[string]any{"type": "string"}},
		))
		ensureServiceTypeWithSpec(ctx, str, "ctr-cel", "container", map[string]any{
			"image":        map[string]any{"reference": "nginx"},
			"database_url": "",
		})
	})

	AfterEach(func() {
		if str != nil {
			Expect(str.Close()).To(Succeed())
		}
	})

	createCatalogItemWithSpec := func(spec v1alpha1.CatalogItemSpec) string {
		ci, err := svc.CatalogItem().Create(ctx, &service.CreateCatalogItemRequest{
			ApiVersion:  "v1alpha1",
			DisplayName: "Dev App CEL",
			Spec:        spec,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(ci.Uid).ToNot(BeNil())
		return *ci.Uid
	}

	instanceCreateReq := func(catalogItemID string) *service.CreateCatalogItemInstanceRequest {
		return &service.CreateCatalogItemInstanceRequest{
			ApiVersion:  "v1alpha1",
			DisplayName: "Instance",
			Spec: v1alpha1.CatalogItemInstanceSpec{
				CatalogItemId: catalogItemID,
				UserValues:    []v1alpha1.UserValue{},
			},
		}
	}

	Describe("catalog item create", func() {
		It("accepts field defaults containing CEL without validating references", func() {
			spec := devAppCatalogItemSpecWithCEL()
			(*spec.Resources[1].Fields)[0].Default = "${missingDb.connectionString}"
			req := &service.CreateCatalogItemRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Deferred CEL",
				Spec:        spec,
			}
			result, err := svc.CatalogItem().Create(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})

	Describe("catalog instance create", func() {
		It("creates instance when CEL defaults validate during merge", func() {
			catalogItemID := createCatalogItemWithSpec(devAppCatalogItemSpecWithCEL())
			result, err := svc.CatalogItemInstance().Create(ctx, instanceCreateReq(catalogItemID))
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})

		It("rejects malformed CEL expressions during merge", func() {
			spec := devAppCatalogItemSpecWithCEL()
			(*spec.Resources[1].Fields)[0].Default = "prefix-${ordersDb.connectionString}"
			catalogItemID := createCatalogItemWithSpec(spec)
			_, err := svc.CatalogItemInstance().Create(ctx, instanceCreateReq(catalogItemID))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrInvalidCELExpression)).To(BeTrue())
		})

		It("rejects CEL referencing unknown catalog resource during merge", func() {
			spec := devAppCatalogItemSpecWithCEL()
			(*spec.Resources[1].Fields)[0].Default = "${missingDb.connectionString}"
			catalogItemID := createCatalogItemWithSpec(spec)
			_, err := svc.CatalogItemInstance().Create(ctx, instanceCreateReq(catalogItemID))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCELResourceNotFound)).To(BeTrue())
		})

		It("rejects CEL referencing unknown service type output during merge", func() {
			spec := devAppCatalogItemSpecWithCEL()
			(*spec.Resources[1].Fields)[0].Default = "${ordersDb.connectionStrng}"
			catalogItemID := createCatalogItemWithSpec(spec)
			_, err := svc.CatalogItemInstance().Create(ctx, instanceCreateReq(catalogItemID))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCELServiceTypeOutputNotFound)).To(BeTrue())
		})

		It("rejects CEL self-reference during merge", func() {
			spec := devAppCatalogItemSpecWithCEL()
			(*spec.Resources[0].Fields)[0].Default = "${ordersDb.connectionString}"
			catalogItemID := createCatalogItemWithSpec(spec)
			_, err := svc.CatalogItemInstance().Create(ctx, instanceCreateReq(catalogItemID))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCELSelfReference)).To(BeTrue())
		})

		It("rejects CEL when source service type declares no outputs during merge", func() {
			ensureServiceTypeWithSpec(ctx, str, "db-no-out", "database-no-outputs", map[string]any{
				"engine": "postgres",
			})
			spec := v1alpha1.CatalogItemSpec{
				Resources: []v1alpha1.CatalogResource{
					{Name: "ordersDb", ServiceType: "database-no-outputs"},
					{
						Name:        "app",
						ServiceType: "container",
						Fields: &[]v1alpha1.FieldConfiguration{
							{Path: "database_url", Default: "${ordersDb.connectionString}"},
						},
					},
				},
			}
			catalogItemID := createCatalogItemWithSpec(spec)
			_, err := svc.CatalogItemInstance().Create(ctx, instanceCreateReq(catalogItemID))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrCELServiceTypeOutputNotFound)).To(BeTrue())
		})

		It("rejects user_values containing CEL expressions", func() {
			catalogItemID := createCatalogItemWithSpec(devAppCatalogItemSpecWithCEL())
			req := &service.CreateCatalogItemInstanceRequest{
				ApiVersion:  "v1alpha1",
				DisplayName: "Bad User CEL",
				Spec: v1alpha1.CatalogItemInstanceSpec{
					CatalogItemId: catalogItemID,
					UserValues: []v1alpha1.UserValue{
						{Resource: "app", Path: "database_url", Value: "${ordersDb.connectionString}"},
					},
				},
			}
			_, err := svc.CatalogItemInstance().Create(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, service.ErrUserValueCELNotAllowed)).To(BeTrue())
		})
	})

	Describe("BuildResourceGraph", func() {
		It("preserves CEL reference in merged spec after validation", func() {
			ci := model.CatalogItem{
				ID:          "graph-cel",
				ApiVersion:  "v1alpha1",
				DisplayName: "Graph CEL",
				Path:        "catalog-items/graph-cel",
				Spec: model.CatalogItemSpec{
					Resources: []model.CatalogResource{
						{Name: "ordersDb", ServiceType: "database", Fields: []model.FieldConfiguration{
							{Path: "engine", Default: "postgres"},
						}},
						{
							Name:              "app",
							ServiceType:       "container",
							RequiresResources: []string{"ordersDb"},
							Fields: []model.FieldConfiguration{
								{Path: "database_url", Default: "${ordersDb.connectionString}"},
							},
						},
					},
				},
			}
			_, err := str.CatalogItem().Create(ctx, ci)
			Expect(err).ToNot(HaveOccurred())

			builder := service.NewSpecBuilderForTest(str)
			graph, err := builder.BuildResourceGraph(ctx, "graph-cel", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(graph).To(HaveLen(2))
			Expect(graph[1].Spec["database_url"]).To(Equal("${ordersDb.connectionString}"))
		})
	})
})
